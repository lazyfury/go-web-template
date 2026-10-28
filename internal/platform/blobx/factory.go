package blobx

import (
	"context"
	"fmt"

	"github.com/example/go-web-template/internal/platform/config"
)

// New builds the configured driver. It is the only place that maps config keys
// to a concrete backend, so the file domain never branches on storage.driver.
func New(ctx context.Context, cfg *config.Storage) (Store, error) {
	switch cfg.Driver {
	case DriverLocal:
		return NewLocal(cfg.Local.Root, cfg.Local.BaseURL, []byte(cfg.SigningSecret))
	case DriverS3:
		return NewS3(ctx, &S3Config{
			Endpoint:     cfg.S3.Endpoint,
			Region:       cfg.S3.Region,
			Bucket:       cfg.S3.Bucket,
			AccessKey:    cfg.S3.AccessKey,
			SecretKey:    cfg.S3.SecretKey,
			UsePathStyle: cfg.S3.UsePathStyle,
			PresignTTL:   cfg.S3.PresignTTL,
		})
	default:
		return nil, fmt.Errorf("blobx: unsupported storage.driver %q", cfg.Driver)
	}
}
