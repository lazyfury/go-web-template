package passwd

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h := NewHasher(4)
	digest, err := h.Hash("correct-horse")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if digest == "correct-horse" || strings.Contains(digest, "correct-horse") {
		t.Fatal("digest must not contain the plaintext")
	}
	if !h.Verify(digest, "correct-horse") {
		t.Error("Verify should accept the original password")
	}
	if h.Verify(digest, "wrong-password") {
		t.Error("Verify should reject a different password")
	}
}

func TestVerifyEmptyDigest(t *testing.T) {
	h := NewHasher(4)
	// Accounts provisioned before the auth migration have password_hash = ''.
	if h.Verify("", "anything") {
		t.Error("empty digest must never verify")
	}
	if h.Verify("not-a-bcrypt-hash", "anything") {
		t.Error("malformed digest must never verify")
	}
}

func TestHashRejectsOversizedPassword(t *testing.T) {
	h := NewHasher(4)
	_, err := h.Hash(strings.Repeat("x", MaxPasswordBytes+1))
	if err == nil {
		t.Fatal("password above bcrypt's 72-byte limit must be rejected, not truncated")
	}
	if _, err := h.Hash(strings.Repeat("x", MaxPasswordBytes)); err != nil {
		t.Errorf("72-byte password should be accepted: %v", err)
	}
}

func TestBurnTimingDoesNotPanic(t *testing.T) {
	NewHasher(4).BurnTiming("anything")
	// An invalid cost makes bcrypt fail; BurnTiming must stay a no-op.
	NewHasher(99).BurnTiming("anything")
}
