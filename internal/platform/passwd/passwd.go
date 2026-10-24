// Package passwd hashes and verifies passwords with bcrypt.
//
// It is a leaf package so both the user domain (admin-created accounts) and
// the auth domain (login, password change) share one implementation and one
// cost setting.
package passwd

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// MaxPasswordBytes is bcrypt's hard input limit. Longer inputs are rejected
// instead of silently truncated.
const MaxPasswordBytes = 72

// Hasher hashes passwords with a fixed bcrypt cost.
type Hasher struct {
	cost       int
	dummyHash  []byte
	dummyError error
}

// NewHasher builds a Hasher. A cost outside bcrypt's allowed range makes
// Hash/Verify report an error rather than panic.
func NewHasher(cost int) *Hasher {
	h := &Hasher{cost: cost}
	h.dummyHash, h.dummyError = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), cost)
	return h
}

// Hash returns the bcrypt digest of plain.
func (h *Hasher) Hash(plain string) (string, error) {
	if len(plain) > MaxPasswordBytes {
		return "", fmt.Errorf("password exceeds %d bytes", MaxPasswordBytes)
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(digest), nil
}

// Verify reports whether plain matches digest. It returns false for empty or
// malformed digests (for example the empty hash of a not-yet-provisioned user).
func (h *Hasher) Verify(digest, plain string) bool {
	if digest == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(digest), []byte(plain)) == nil
}

// BurnTiming runs a bcrypt comparison against a fixed dummy digest. Login calls
// it for unknown emails so "user not found" and "wrong password" take the same
// time and leak nothing through timing.
func (h *Hasher) BurnTiming(plain string) {
	if h.dummyError != nil {
		return
	}
	_ = bcrypt.CompareHashAndPassword(h.dummyHash, []byte(plain))
}
