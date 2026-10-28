package authkit

import (
	"testing"
	"time"
)

func benchmarkIssuer(tb testing.TB, retired int) *TokenIssuer {
	tb.Helper()

	keys := []Key{{ID: "k1", Secret: []byte("benchmark-secret-0123456789abcdef")}}
	for i := 0; i < retired; i++ {
		keys = append(keys, Key{Secret: []byte("retired-secret-0123456789abcdef")})
	}
	issuer, err := NewTokenIssuerWithKeys(keys, "go-web-template", 15*time.Minute)
	if err != nil {
		tb.Fatalf("NewTokenIssuerWithKeys: %v", err)
	}
	return issuer
}

// BenchmarkTokenIssue is the per-login cost (HMAC-SHA256 + JSON claims).
func BenchmarkTokenIssue(b *testing.B) {
	issuer := benchmarkIssuer(b, 0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := issuer.Issue(42, RoleMember, 3); err != nil {
			b.Fatalf("Issue: %v", err)
		}
	}
}

// BenchmarkTokenParse is the per-authenticated-request CPU cost, independent of
// the DB read that VerifyAccess does on top of it.
func BenchmarkTokenParse(b *testing.B) {
	issuer := benchmarkIssuer(b, 0)
	raw, err := issuer.Issue(42, RoleMember, 3)
	if err != nil {
		b.Fatalf("Issue: %v", err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := issuer.Parse(raw); err != nil {
			b.Fatalf("Parse: %v", err)
		}
	}
}

// BenchmarkTokenParseKeyRing covers the rotation case: a `kid`-less token has to
// try the retired keys one by one, which is the worst case during a rotation.
func BenchmarkTokenParseKeyRing(b *testing.B) {
	issuer := benchmarkIssuer(b, 4)
	raw, err := issuer.Issue(42, RoleMember, 3)
	if err != nil {
		b.Fatalf("Issue: %v", err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := issuer.Parse(raw); err != nil {
			b.Fatalf("Parse: %v", err)
		}
	}
}

// FuzzTokenParse feeds arbitrary strings through verification: it must never
// panic, and only a token this issuer actually signed may authenticate.
func FuzzTokenParse(f *testing.F) {
	issuer := benchmarkIssuer(f, 0)
	valid, err := issuer.Issue(42, RoleAdmin, 7)
	if err != nil {
		f.Fatalf("Issue: %v", err)
	}
	f.Add(valid)
	f.Add("")
	f.Add("a.b.c")
	f.Add("eyJhbGciOiJub25lIn0.eyJzdWIiOiIxIn0.")

	f.Fuzz(func(t *testing.T, raw string) {
		principal, err := issuer.Parse(raw)
		if err == nil {
			if raw != valid {
				t.Fatalf("Parse accepted an unsigned token: %q", raw)
			}
			if principal == nil || principal.UserID != 42 || principal.Role != RoleAdmin {
				t.Fatalf("principal = %+v", principal)
			}
		}
		if err == nil && principal == nil {
			t.Fatal("Parse returned nil principal with nil error")
		}
	})
}
