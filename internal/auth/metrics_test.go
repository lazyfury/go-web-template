package auth

import (
	"context"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/testutil"
	"github.com/example/go-web-template/internal/user"
)

// TestLoginMetricsCoverEveryOutcome 保证每个登录分支都有可告警的结局：HTTP 层
// 只能是 401，运维要区分「暴力猜密码」和「有人被锁了」只能靠这些标签。
func TestLoginMetricsCoverEveryOutcome(t *testing.T) {
	h := newHarness(t)
	active := h.seedUser(t, "metric-member@example.com", authkit.RoleMember, user.StatusActive)
	h.seedUser(t, "metric-disabled@example.com", authkit.RoleMember, user.StatusDisabled)

	before := map[string]float64{}
	for _, result := range []string{"success", "bad_password", "locked_out", "locked", "unknown_email", "disabled"} {
		before[result] = testutil.CounterValueFor(t, "auth_login_total", "result", result)
	}
	lockouts := testutil.CounterValue(t, "auth_account_lockouts_total")

	// success
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: active.Email, Password: testPassword}); err != nil {
		t.Fatalf("successful login: %v", err)
	}
	// bad_password
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: active.Email, Password: "wrong-password"}); err == nil {
		t.Fatal("wrong password must fail")
	}
	// unknown_email
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: "nobody-metric@example.com", Password: testPassword}); err == nil {
		t.Fatal("unknown email must fail")
	}
	// disabled
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: "metric-disabled@example.com", Password: testPassword}); err == nil {
		t.Fatal("disabled account must fail")
	}
	// locked_out: max attempts (3 in the harness) then the lock itself.
	h.users.users[active.ID].FailedLoginCount = 2
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: active.Email, Password: "wrong-password"}); err == nil {
		t.Fatal("third failure must fail")
	}
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: active.Email, Password: testPassword}); err == nil {
		t.Fatal("locked account must fail")
	}

	for result, want := range map[string]float64{
		"success":       1,
		"bad_password":  1,
		"locked_out":    1,
		"locked":        1,
		"unknown_email": 1,
		"disabled":      1,
	} {
		got := testutil.CounterValueFor(t, "auth_login_total", "result", result) - before[result]
		if got < want {
			t.Errorf("auth_login_total{result=%q} increased by %v, want at least %v", result, got, want)
		}
	}
	if got := testutil.CounterValue(t, "auth_account_lockouts_total") - lockouts; got < 1 {
		t.Errorf("auth_account_lockouts_total increased by %v, want at least 1", got)
	}
}

func TestRefreshMetricsCoverEveryOutcome(t *testing.T) {
	h := newHarness(t)
	account := h.seedUser(t, "refresh-metric@example.com", authkit.RoleMember, user.StatusActive)

	results := []string{"ok", "tolerated_replay", "expired", "invalid"}
	before := make(map[string]float64, len(results))
	for _, result := range results {
		before[result] = testutil.CounterValueFor(t, "auth_refresh_total", "result", result)
	}
	// invalid: never issued.
	if _, err := h.svc.Refresh(context.Background(), "not-a-real-token", ClientMeta{}); err == nil {
		t.Fatal("unknown refresh token must fail")
	}

	session, err := h.svc.Login(context.Background(), LoginInput{Email: account.Email, Password: testPassword})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	// ok: rotate once.
	rotated, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// tolerated_replay: replaying the rotated-away token inside the window.
	if _, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{}); err != nil {
		t.Fatalf("tolerated replay: %v", err)
	}

	// expired: a token that is in the store but past its expiry.
	expiredRaw := "expired-raw-token"
	if err := h.tokens.Create(context.Background(), &RefreshToken{
		UserID:           account.ID,
		TokenHash:        hashToken(expiredRaw),
		ExpiresAt:        h.now.Add(-time.Minute),
		SessionStartedAt: h.now.Add(-time.Hour),
		CreatedAt:        h.now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("create expired token: %v", err)
	}
	if _, err := h.svc.Refresh(context.Background(), expiredRaw, ClientMeta{}); err == nil {
		t.Fatal("expired refresh token must fail")
	}
	if rotated.RefreshToken == "" {
		t.Fatal("rotation returned no token")
	}

	for result, want := range map[string]float64{"ok": 1, "tolerated_replay": 1, "expired": 1, "invalid": 1} {
		got := testutil.CounterValueFor(t, "auth_refresh_total", "result", result) - before[result]
		if got < want {
			t.Errorf("auth_refresh_total{result=%q} increased by %v, want at least %v", result, got, want)
		}
	}
}
