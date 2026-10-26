package authkit

import (
	"errors"
	"fmt"
	"strconv"
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

// Key is one HMAC-SHA256 signing key. ID is stamped as the JWT "kid" header on
// issued tokens; it may be empty for keys that exist only to verify tokens
// signed before a rotation.
type Key struct {
	ID     string
	Secret []byte
}

// TokenIssuer issues and verifies HS256 access tokens.
//
// The signing method is pinned on both sides (never trust the "alg" header),
// the issuer is validated and expiration is mandatory: a token crafted with
// alg=none, HS512 or another issuer is rejected before any claim is read.
//
// The ring's first key signs; the remaining keys are verification-only, which is
// what makes a rotation non-disruptive: put the new secret first, keep the old
// one behind it, and tokens issued before the switch stay valid until they
// expire on their own.
type TokenIssuer struct {
	keys   []Key
	byID   map[string][]byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenIssuer builds a single-key issuer. ttl must be positive.
func NewTokenIssuer(secret, issuer string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{
		keys:   []Key{{Secret: []byte(secret)}},
		byID:   map[string][]byte{},
		issuer: issuer,
		ttl:    ttl,
		now:    time.Now,
	}
}

// NewTokenIssuerWithKeys builds an issuer over a key ring: keys[0] signs, the
// rest only verify. Secrets must be non-empty and non-empty ids must be unique.
func NewTokenIssuerWithKeys(keys []Key, issuer string, ttl time.Duration) (*TokenIssuer, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("authkit: at least one signing key is required")
	}
	ring := make([]Key, 0, len(keys))
	byID := make(map[string][]byte, len(keys))
	for i, key := range keys {
		if len(key.Secret) == 0 {
			return nil, fmt.Errorf("authkit: signing key %d has an empty secret", i)
		}
		if key.ID != "" {
			if _, dup := byID[key.ID]; dup {
				return nil, fmt.Errorf("authkit: duplicate signing key id %q", key.ID)
			}
			byID[key.ID] = key.Secret
		}
		ring = append(ring, Key{ID: key.ID, Secret: append([]byte(nil), key.Secret...)})
	}
	return &TokenIssuer{keys: ring, byID: byID, issuer: issuer, ttl: ttl, now: time.Now}, nil
}

// TTL returns the access-token lifetime.
func (i *TokenIssuer) TTL() time.Duration { return i.ttl }

// Issue signs an access token for the given user with the ring's first key.
func (i *TokenIssuer) Issue(userID uint64, role Role, tokenVersion int) (string, error) {
	if len(i.keys) == 0 || len(i.keys[0].Secret) == 0 {
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
	key := i.keys[0]
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	if key.ID != "" {
		token.Header["kid"] = key.ID
	}
	signed, err := token.SignedString(key.Secret)
	if err != nil {
		return "", fmt.Errorf("authkit: sign token: %w", err)
	}
	return signed, nil
}

// Parse verifies an access token against the key ring and returns the principal
// it carries. Any failure is reported as apperr.CodeUnauthorized with a stable
// message that does not reveal *why* the token was rejected.
func (i *TokenIssuer) Parse(raw string) (*Principal, error) {
	var claims *Claims
	var verifyErr error
	for _, key := range i.candidateKeys(raw) {
		claims = &Claims{}
		_, err := i.parser().ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
			return key.Secret, nil
		})
		if err != nil {
			// Prefer the specific reason: an expired token must keep saying so
			// instead of degrading into a signature mismatch on the next key.
			if verifyErr == nil || errors.Is(err, jwt.ErrTokenExpired) {
				verifyErr = err
			}
			continue
		}
		return principalFromClaims(claims)
	}
	if verifyErr == nil {
		verifyErr = fmt.Errorf("authkit: no verification key")
	}
	if errors.Is(verifyErr, jwt.ErrTokenExpired) {
		return nil, apperr.Wrap(apperr.CodeUnauthorized, "access token expired", verifyErr)
	}
	return nil, apperr.Wrap(apperr.CodeUnauthorized, "invalid access token", verifyErr)
}

func (i *TokenIssuer) parser() *jwt.Parser {
	return jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(i.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockSkew),
		// Same clock as Issue, so tests can move time deterministically.
		jwt.WithTimeFunc(i.now),
	)
}

// candidateKeys returns the keys worth trying for raw. A token whose "kid" names
// a known key only needs that one; anything else (no kid, unknown kid, tokens
// issued before the rotation) falls back to the whole ring. The header is never
// trusted: a forged "kid" only costs a signature mismatch.
func (i *TokenIssuer) candidateKeys(raw string) []Key {
	unverified, _, err := new(jwt.Parser).ParseUnverified(raw, &Claims{})
	if err != nil {
		return i.keys
	}
	kid, _ := unverified.Header["kid"].(string)
	if kid == "" {
		return i.keys
	}
	secret, ok := i.byID[kid]
	if !ok {
		return i.keys
	}
	return []Key{{ID: kid, Secret: secret}}
}

// principalFromClaims turns already-verified claims into a principal.
func principalFromClaims(claims *Claims) (*Principal, error) {
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

// parseUserID requires the subject to be the canonical decimal form of a
// non-zero user id. Sscanf-style parsing accepted trailing garbage ("1 or 1=1"
// → 1), which silently widened the set of tokens that resolve to a real user.
func parseUserID(subject string) (uint64, error) {
	id, err := strconv.ParseUint(subject, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != subject {
		return 0, fmt.Errorf("invalid subject %q", subject)
	}
	return id, nil
}
