package authkit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/httpx"
)

const testSecret = "test-secret-that-is-long-enough-for-hs256"

func newIssuer(ttl time.Duration) *TokenIssuer {
	issuer := NewTokenIssuer(testSecret, "go-web-template-test", ttl)
	issuer.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return issuer
}

func TestTokenRoundTrip(t *testing.T) {
	issuer := newIssuer(15 * time.Minute)
	raw, err := issuer.Issue(42, RoleAdmin, 3)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	principal, err := issuer.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if principal.UserID != 42 || principal.Role != RoleAdmin || principal.TokenVersion != 3 {
		t.Errorf("principal = %+v", principal)
	}
}

func TestTokenRejectsExpired(t *testing.T) {
	issuer := newIssuer(15 * time.Minute)
	raw, err := issuer.Issue(42, RoleAdmin, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Move the verifier clock past ttl + leeway.
	issuer.now = func() time.Time {
		return time.Unix(1_700_000_000, 0).Add(15*time.Minute + clockSkew + time.Second).UTC()
	}
	_, err = issuer.Parse(raw)
	if !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("expired token should be unauthorized, got %v", err)
	}
}

func TestTokenRejectsWrongIssuer(t *testing.T) {
	other := NewTokenIssuer(testSecret, "someone-else", time.Hour)
	raw, err := other.Issue(1, RoleMember, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := newIssuer(time.Hour).Parse(raw); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("wrong issuer should be rejected, got %v", err)
	}
}

func TestTokenRejectsWrongSecret(t *testing.T) {
	other := NewTokenIssuer("another-secret-that-is-long-enough", "go-web-template-test", time.Hour)
	raw, err := other.Issue(1, RoleMember, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := newIssuer(time.Hour).Parse(raw); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("wrong secret should be rejected, got %v", err)
	}
}

func TestTokenRejectsAlgNone(t *testing.T) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "go-web-template-test",
			Subject:   "1",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Role: RoleAdmin,
		Type: TokenTypeAccess,
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("craft none token: %v", err)
	}
	if _, err := newIssuer(time.Hour).Parse(unsigned); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("alg=none must be rejected, got %v", err)
	}
}

func TestTokenRejectsRefreshTyp(t *testing.T) {
	issuer := newIssuer(time.Hour)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "go-web-template-test",
			Subject:   "1",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Role: RoleAdmin,
		Type: "refresh",
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("craft token: %v", err)
	}
	if _, err := issuer.Parse(raw); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("non-access typ must be rejected, got %v", err)
	}
}

func TestIssueRejectsEmptySecretAndBadRole(t *testing.T) {
	if _, err := NewTokenIssuer("", "iss", time.Hour).Issue(1, RoleMember, 0); err == nil {
		t.Error("empty secret must not sign tokens")
	}
	issuer := newIssuer(time.Hour)
	if _, err := issuer.Issue(1, Role("root"), 0); err == nil {
		t.Error("unknown role must not be signed")
	}
}

func TestRBAC(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleAdmin, PermUsersRead, true},
		{RoleAdmin, PermUsersDelete, true},
		{RoleAdmin, PermUsersReadSelf, false},
		{RoleMember, PermUsersRead, false},
		{RoleMember, PermUsersReadSelf, true},
		{RoleMember, PermUsersWriteSelf, true},
		{RoleMember, PermUsersDelete, false},
		{Role("unknown"), PermUsersRead, false},
	}
	for _, tc := range cases {
		if got := Can(tc.role, tc.perm); got != tc.want {
			t.Errorf("Can(%q, %q) = %v, want %v", tc.role, tc.perm, got, tc.want)
		}
	}
	if !CanAny(RoleMember, PermUsersRead, PermUsersReadSelf) {
		t.Error("CanAny should match the self permission")
	}
	if len(Permissions(RoleAdmin)) == 0 {
		t.Error("admin must have permissions")
	}
}

// fakeVerifier returns a fixed principal for the token "good".
type fakeVerifier struct{}

func (fakeVerifier) VerifyAccess(_ context.Context, raw string) (*Principal, error) {
	if raw != "good" {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid access token")
	}
	return &Principal{UserID: 9, Role: RoleMember, TokenVersion: 0}, nil
}

func newTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	protected := engine.Group("/protected", Authenticate(fakeVerifier{}))
	protected.GET("/me", func(c *gin.Context) {
		principal, _ := PrincipalFrom(c)
		c.JSON(http.StatusOK, principal)
	})
	protected.DELETE("/admin", RequirePermission(PermUsersDelete), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return engine
}

func TestAuthenticateMiddleware(t *testing.T) {
	engine := newTestEngine()

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"not bearer", "Basic abc", http.StatusUnauthorized},
		{"empty bearer", "Bearer ", http.StatusUnauthorized},
		{"invalid token", "Bearer nope", http.StatusUnauthorized},
		{"valid token", "Bearer good", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected/me", http.NoBody)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusUnauthorized {
				var body httpx.Body
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode envelope: %v", err)
				}
				if body.Code != apperr.CodeUnauthorized {
					t.Errorf("code = %d, want %d", body.Code, apperr.CodeUnauthorized)
				}
			}
		})
	}
}

func TestRequirePermissionMiddleware(t *testing.T) {
	engine := newTestEngine()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/protected/admin", http.NoBody)
	req.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	var body httpx.Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if body.Code != apperr.CodeForbidden {
		t.Errorf("code = %d, want %d", body.Code, apperr.CodeForbidden)
	}
}

// TestTokenRejectsNonCanonicalSubject 钉住 F10：subject 必须是用户 id 的规范十进制
// 形式，`1 or 1=1` / ` 1` / `01` 这类宽松解析过的值一律拒绝。
func TestTokenRejectsNonCanonicalSubject(t *testing.T) {
	t.Parallel()
	issuer := newIssuer(15 * time.Minute)

	for _, subject := range []string{"1 or 1=1", " 1", "1 ", "01", "+1", "1e3", "0", "", "-1"} {
		claims := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "go-web-template-test",
				Subject:   subject,
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
			},
			Role:         RoleAdmin,
			TokenVersion: 0,
			Type:         TokenTypeAccess,
		}
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
		if err != nil {
			t.Fatalf("sign subject %q: %v", subject, err)
		}
		if _, err := issuer.Parse(raw); err == nil {
			t.Errorf("subject %q must be rejected", subject)
		}
	}

	// 规范形式仍然可用。
	raw, err := issuer.Issue(42, RoleAdmin, 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	principal, err := issuer.Parse(raw)
	if err != nil || principal.UserID != 42 {
		t.Fatalf("canonical subject rejected: %v %+v", err, principal)
	}
}
