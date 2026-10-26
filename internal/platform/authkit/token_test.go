package authkit

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/example/go-web-template/internal/platform/apperr"
)

const (
	rotationSecretA = "rotation-secret-a-long-enough-for-hs256"
	rotationSecretB = "rotation-secret-b-long-enough-for-hs256"
	testIssuerName  = "go-web-template-test"
)

func newRing(t *testing.T, keys []Key) *TokenIssuer {
	t.Helper()
	issuer, err := NewTokenIssuerWithKeys(keys, testIssuerName, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewTokenIssuerWithKeys: %v", err)
	}
	issuer.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return issuer
}

// TestTokenKeyRingKeepsRetiredTokensValid pins the rotation story: the old
// secret moves behind the new one and tokens issued before the switch keep
// working until they expire, so a rotation does not log everyone out.
func TestTokenKeyRingKeepsRetiredTokensValid(t *testing.T) {
	before := NewTokenIssuer(rotationSecretA, testIssuerName, 15*time.Minute)
	before.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	oldToken, err := before.Issue(7, RoleAdmin, 2)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	rotated := newRing(t, []Key{
		{ID: "k2", Secret: []byte(rotationSecretB)},
		{ID: "k1", Secret: []byte(rotationSecretA)},
	})
	principal, err := rotated.Parse(oldToken)
	if err != nil {
		t.Fatalf("token signed by the retired key must still verify: %v", err)
	}
	if principal.UserID != 7 || principal.TokenVersion != 2 {
		t.Errorf("principal = %+v", principal)
	}

	fresh, err := rotated.Issue(7, RoleAdmin, 2)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	unverified, _, err := new(jwt.Parser).ParseUnverified(fresh, &Claims{})
	if err != nil {
		t.Fatalf("parse fresh header: %v", err)
	}
	if got := unverified.Header["kid"]; got != "k2" {
		t.Errorf("kid header = %v, want k2 (the active key)", got)
	}
	// The signing key changed: an issuer that only knows the old secret must
	// reject the new token, which is what proves the rotation actually rotated.
	if _, err := before.Parse(fresh); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("old key must not verify the new token, got %v", err)
	}
}

// TestTokenKeyRingIgnoresForgedKeyID makes sure "kid" is a hint for selection,
// never a trust signal: a token that names the active kid but is signed with an
// untrusted key is rejected.
func TestTokenKeyRingIgnoresForgedKeyID(t *testing.T) {
	ring := newRing(t, []Key{
		{ID: "k2", Secret: []byte(rotationSecretB)},
		{ID: "k1", Secret: []byte(rotationSecretA)},
	})

	for _, kid := range []string{"k2", "k1", "unknown", ""} {
		claims := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    testIssuerName,
				Subject:   "7",
				IssuedAt:  jwt.NewNumericDate(time.Unix(1_700_000_000, 0)),
				ExpiresAt: jwt.NewNumericDate(time.Unix(1_700_000_000, 0).Add(time.Hour)),
			},
			Role: RoleAdmin,
			Type: TokenTypeAccess,
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		if kid != "" {
			token.Header["kid"] = kid
		}
		raw, err := token.SignedString([]byte("attacker-secret-that-is-long-enough"))
		if err != nil {
			t.Fatalf("craft token: %v", err)
		}
		if _, err := ring.Parse(raw); !errors.Is(err, apperr.ErrUnauthorized) {
			t.Errorf("kid %q with a foreign signature must be rejected, got %v", kid, err)
		}
	}
}

// TestTokenKeyRingWithoutKeyIDStillVerifies covers the upgrade path: tokens
// issued before kid was configured carry no header at all.
func TestTokenKeyRingWithoutKeyIDStillVerifies(t *testing.T) {
	issuer := NewTokenIssuer(rotationSecretA, testIssuerName, 15*time.Minute)
	issuer.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	raw, err := issuer.Issue(3, RoleMember, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	ring := newRing(t, []Key{
		{ID: "k1", Secret: []byte(rotationSecretA)},
		{Secret: []byte(rotationSecretB)},
	})
	if _, err := ring.Parse(raw); err != nil {
		t.Fatalf("kid-less token must verify: %v", err)
	}
}

// TestTokenKeyRingReportsExpired pins that the multi-key loop still reports the
// specific reason instead of degrading a expired token into a signature error.
func TestTokenKeyRingReportsExpired(t *testing.T) {
	ring := newRing(t, []Key{
		{ID: "k2", Secret: []byte(rotationSecretB)},
		{ID: "k1", Secret: []byte(rotationSecretA)},
	})
	raw, err := ring.Issue(5, RoleMember, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ring.now = func() time.Time {
		return time.Unix(1_700_000_000, 0).Add(15*time.Minute + clockSkew + time.Second).UTC()
	}
	_, err = ring.Parse(raw)
	if !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("expired token must be unauthorized, got %v", err)
	}
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeUnauthorized {
		t.Fatalf("error = %v, want CodeUnauthorized", err)
	}
	if appErr.Message != "access token expired" {
		t.Errorf("message = %q, want the expired reason", appErr.Message)
	}
}

func TestNewTokenIssuerWithKeysRejectsInvalidRings(t *testing.T) {
	cases := []struct {
		name string
		keys []Key
	}{
		{name: "no keys", keys: nil},
		{name: "empty secret", keys: []Key{{ID: "k1", Secret: []byte(rotationSecretA)}, {ID: "k2"}}},
		{name: "duplicate id", keys: []Key{{ID: "k1", Secret: []byte(rotationSecretA)}, {ID: "k1", Secret: []byte(rotationSecretB)}}},
	}
	for _, tc := range cases {
		if _, err := NewTokenIssuerWithKeys(tc.keys, testIssuerName, time.Minute); err == nil {
			t.Errorf("%s: ring must be rejected", tc.name)
		}
	}
	// Empty ids are allowed on verify-only keys (a tenant of the configured
	// retired secrets), but not twice for the same key material.
	if _, err := NewTokenIssuerWithKeys([]Key{
		{ID: "k2", Secret: []byte(rotationSecretB)},
		{Secret: []byte(rotationSecretA)},
	}, testIssuerName, time.Minute); err != nil {
		t.Errorf("ring with an id-less retired key must be accepted: %v", err)
	}
}
