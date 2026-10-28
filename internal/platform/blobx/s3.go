package blobx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
)

// S3Config configures the S3-compatible driver. Aliyun OSS, MinIO, R2 and AWS
// S3 all speak this API; there is deliberately no vendor-specific driver.
type S3Config struct {
	// Endpoint is the S3 API base URL; empty means AWS.
	Endpoint string
	Region   string
	Bucket   string
	// AccessKey/SecretKey are optional: empty falls back to the SDK's default
	// credential chain (env vars, IAM role), which production should use.
	AccessKey string
	SecretKey string
	// UsePathStyle forces bucket-in-path addressing (MinIO and some OSS setups).
	UsePathStyle bool
	// PresignTTL is the default signed-URL lifetime.
	PresignTTL time.Duration
}

// S3 is an S3-compatible Store implementation.
type S3 struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	presign   time.Duration
}

// NewS3 builds the S3 driver. It only loads the SDK config (no network call):
// an unreachable endpoint must fail the first upload with a clear error, not
// block startup.
func NewS3(ctx context.Context, cfg *S3Config) (*S3, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("blobx: s3 bucket must not be empty")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if cfg.AccessKey != "" || cfg.SecretKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("blobx: load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.UsePathStyle
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		// Streaming uploads are not seekable, so the SDK cannot pre-compute a
		// trailing checksum. Let it skip the checksum trailer instead of buffering
		// the whole file in memory.
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		options.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	ttl := clampExpiry(cfg.PresignTTL, 10*time.Minute)
	return &S3{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
		presign:   ttl,
	}, nil
}

// Driver implements Store.
func (s *S3) Driver() string { return DriverS3 }

// Put implements Store.
func (s *S3) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) (ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		return ObjectInfo{}, fmt.Errorf("blobx: s3 put object: %w", err)
	}
	return ObjectInfo{
		Key:         key,
		Size:        size,
		ContentType: contentType,
		ModTime:     time.Now(),
	}, nil
}

// Open implements Store.
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, ObjectInfo{}, mapS3Error(err, "get object")
	}
	info := ObjectInfo{Key: key}
	if output.ContentLength != nil {
		info.Size = *output.ContentLength
	}
	if output.ContentType != nil {
		info.ContentType = *output.ContentType
	}
	if output.LastModified != nil {
		info.ModTime = *output.LastModified
	}
	return output.Body, info, nil
}

// Stat implements Store.
func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return ObjectInfo{}, mapS3Error(err, "head object")
	}
	info := ObjectInfo{Key: key}
	if output.ContentLength != nil {
		info.Size = *output.ContentLength
	}
	if output.ContentType != nil {
		info.ContentType = *output.ContentType
	}
	if output.LastModified != nil {
		info.ModTime = *output.LastModified
	}
	return info, nil
}

// Delete implements Store. A missing object is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		if errors.Is(mapS3Error(err, "delete object"), ErrNotFound) {
			return nil
		}
		return mapS3Error(err, "delete object")
	}
	return nil
}

// URL implements Store: a presigned GET whose response headers carry the
// disposition, so the browser downloads with the original filename.
func (s *S3) URL(_ context.Context, key string, opt SignedURLOptions) (string, time.Time, error) {
	if err := ValidateKey(key); err != nil {
		return "", time.Time{}, err
	}
	expiry := clampExpiry(opt.Expiry, s.presign)
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if opt.Disposition != "" && opt.Filename != "" {
		input.ResponseContentDisposition = aws.String(contentDisposition(opt.Disposition, opt.Filename))
	} else if opt.Disposition != "" {
		input.ResponseContentDisposition = aws.String(opt.Disposition)
	}
	presigned, err := s.presigner.PresignGetObject(context.Background(), input, func(options *s3.PresignOptions) {
		options.Expires = expiry
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("blobx: presign get object: %w", err)
	}
	if _, err := url.Parse(presigned.URL); err != nil {
		return "", time.Time{}, fmt.Errorf("blobx: presigned URL is invalid: %w", err)
	}
	return presigned.URL, time.Now().Add(expiry), nil
}

// mapS3Error converts a 404/NoSuchKey into ErrNotFound so callers do not have
// to know the SDK's error taxonomy.
func mapS3Error(err error, action string) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket":
			return fmt.Errorf("%w: %s: %s", ErrNotFound, action, apiErr.ErrorCode())
		}
	}
	var responseErr interface{ HTTPStatusCode() int }
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == 404 {
		return fmt.Errorf("%w: %s", ErrNotFound, action)
	}
	return fmt.Errorf("blobx: %s: %w", action, err)
}

// contentDisposition builds an RFC 6266 header value with an ASCII fallback and
// a UTF-8 filename* so non-ASCII names survive.
func contentDisposition(disposition, filename string) string {
	if filename == "" {
		return disposition
	}
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, filename)
	return fmt.Sprintf(`%s; filename=%q; filename*=UTF-8''%s`, disposition, fallback, url.PathEscape(filename))
}
