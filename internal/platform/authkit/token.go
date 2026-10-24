package authkit

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// TokenTypeAccess is the "typ" claim stamped on access tokens. Refresh tokens
// are opaque random strings, never JWTs, so the two cannot be confused.
const TokenTypeAccess = "access"

// clockSkew tolerates small time differences between issuer and verifier.
const clockSkew = 30 * time.Second

// Claims is the access-token payload.
type Claims struct {
	jwt.RegisteredClaims
	Role         Role   `json:"role"`
	TokenVersion int    `json:"ver"`
	Type         string `json:"typ"`
}

// TokenIssuer issues and verifies HS256 access tokens.
//
// The signing method is pinned on both sides (never trust the "alg" header),
// the issuer is validated and expiration is mandatory: a token crafted with
// alg=none, HS512 or another issuer is rejected before any claim is read.
type TokenIssuer struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenIssuer builds an issuer. ttl must be positive.
func NewTokenIssuer(secret, issuer string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{
		secret: []byte(secret),
		issuer: issuer,
		ttl:    ttl,
		now:    time.Now,
	}
}

// TTL returns the access-token lifetime.
func (i *TokenIssuer) TTL() time.Duration { return i.ttl }

// Issue signs an access token for the given user.
func (i *TokenIssuer) Issue(userID uint64, role Role, tokenVersion int) (string, error) {
	if len(i.secret) == 0 {
		return "", fmt.Errorf("authkit: empty signing secret")
	}
	if !role.Valid() {
		return "", fmt.Errorf("authkit: invalid role %q", role)
	}
	now := i.now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
		Role:         role,
		TokenVersion: tokenVersion,
		Type:         TokenTypeAccess,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("authkit: sign token: %w", err)
	}
	return signed, nil
}

// Parse verifies an access token and returns the principal it carries. Any
// failure is reported as apperr.CodeUnauthorized with a stable message that
// does not reveal *why* the token was rejected.
func (i *TokenIssuer) Parse(raw string) (*Principal, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(i.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockSkew),
		// Same clock as Issue, so tests can move time deterministically.
		jwt.WithTimeFunc(i.now),
	)

	claims := &Claims{}
	if _, err := parser.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return i.secret, nil
	}); err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, apperr.Wrap(apperr.CodeUnauthorized, "access token expired", err)
		default:
			return nil, apperr.Wrap(apperr.CodeUnauthorized, "invalid access token", err)
		}
	}
	if claims.Type != TokenTypeAccess {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid access token")
	}
	userID, err := claims.GetSubject()
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeUnauthorized, "invalid access token", err)
	}
	id, err := parseUserID(userID)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeUnauthorized, "invalid access token", err)
	}
	if !claims.Role.Valid() {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid access token")
	}
	return &Principal{UserID: id, Role: claims.Role, TokenVersion: claims.TokenVersion}, nil
}

func parseUserID(subject string) (uint64, error) {
	var id uint64
	if _, err := fmt.Sscanf(subject, "%d", &id); err != nil || id == 0 {
		return 0, fmt.Errorf("invalid subject %q", subject)
	}
	return id, nil
}
