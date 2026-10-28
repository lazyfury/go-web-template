package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.App.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.App.Port)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("default driver = %q, want postgres", cfg.Database.Driver)
	}
	if cfg.HTTPServer.RequestTimeout != 15*time.Second {
		t.Errorf("default request timeout = %v, want 15s", cfg.HTTPServer.RequestTimeout)
	}
	if cfg.HTTPServer.MaxBodyBytes != 1<<20 {
		t.Errorf("default max body bytes = %d, want 1MiB", cfg.HTTPServer.MaxBodyBytes)
	}
	if cfg.HTTPServer.MaxHeaderBytes != 16<<10 {
		t.Errorf("default max header bytes = %d, want 16KiB", cfg.HTTPServer.MaxHeaderBytes)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
app:
  env: staging
  port: 9090
log:
  level: debug
  format: console
ratelimit:
  enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path, "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.App.Env != "staging" {
		t.Errorf("env = %q, want staging", cfg.App.Env)
	}
	if cfg.App.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.App.Port)
	}
	// Values not in the file keep their defaults.
	if cfg.App.Name != "go-web-template" {
		t.Errorf("name = %q, want default", cfg.App.Name)
	}
	if cfg.RateLimit.Enabled {
		t.Error("ratelimit.enabled should be false")
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("APP_DATABASE_HOST", "db.internal")
	t.Setenv("APP_DATABASE_PORT", "5433")
	t.Setenv("APP_LOG_LEVEL", "warn")
	t.Setenv("APP_CORS_ALLOWED_ORIGINS", "https://a.example,https://b.example")

	// Use a config file that sets conflicting values; env must win.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
database:
  host: 127.0.0.1
  port: 5432
log:
  level: info
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path, "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Host != "db.internal" {
		t.Errorf("db host = %q, want env override", cfg.Database.Host)
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("db port = %d, want 5433", cfg.Database.Port)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log level = %q, want warn", cfg.Log.Level)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 || cfg.CORS.AllowedOrigins[0] != "https://a.example" {
		t.Errorf("cors origins = %v, want env override", cfg.CORS.AllowedOrigins)
	}
}

func TestLoadExplicitMissingFileFails(t *testing.T) {
	// An explicitly requested config file must fail fast instead of silently
	// falling back to defaults.
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"), "APP")
	if err == nil {
		t.Fatal("expected error when explicit config file is missing")
	}
}

func TestLoadDefaultMissingFileFallsBack(t *testing.T) {
	// When no explicit file is given and configs/config.yaml is absent, the
	// loader must fall back to defaults + env vars.
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("missing default file should not fail: %v", err)
	}
	if cfg.App.Name != "go-web-template" {
		t.Errorf("expected defaults, got %+v", cfg.App)
	}
}

func TestValidateRejectsBadPort(t *testing.T) {
	cfg := Defaults()
	cfg.App.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for port 0")
	}
}

func TestLoadEnvOverridesAppSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("app:\n  port: 8080\n  debug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// README documents APP_PORT (not APP_APP_PORT): the "app" section must not
	// double the prefix.
	t.Setenv("APP_PORT", "9999")
	t.Setenv("APP_DEBUG", "false")

	cfg, err := Load(path, "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.App.Port != 9999 {
		t.Errorf("port = %d, want env override 9999", cfg.App.Port)
	}
	if cfg.App.Debug {
		t.Error("debug = true, want env override false")
	}
}

func TestValidateRejectsPublicSecretsInProduction(t *testing.T) {
	// 下面每个字面量都曾出现在本仓库里（compose 默认值 / configs / .env.example），
	// 拿到仓库的人就能用它自签 admin token——生产必须拒绝。
	secrets := []string{
		"local-compose-default-jwt-secret-32b",      // 旧 deploy/docker-compose.yml 默认值
		"dev-only-insecure-secret-change-me-32b",    // configs/config.yaml
		"CHANGE_ME_use_a_random_32+_byte_secret",    // configs/config.example.yaml
		"replace-with-a-random-32-plus-byte-secret", // .env.example
		"please_use_your-secret_here_32bytes_ok",    // your-secret 标记
	}
	for _, secret := range secrets {
		cfg := Defaults()
		cfg.App.Env = "production"
		cfg.Auth.JWTSecret = secret
		if err := cfg.Validate(); err == nil {
			t.Errorf("production accepted public secret %q", secret)
		}
	}
}

func TestValidateAcceptsRandomSecretInProduction(t *testing.T) {
	cfg := Defaults()
	cfg.App.Env = "production"
	cfg.Auth.JWTSecret = "Qk3f9vT2zR7pLmW1sXc8nB4hYd6gJa0eUq5rItOy"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("random secret rejected in production: %v", err)
	}
}

func TestValidateAllowsPublicSecretOutsideProduction(t *testing.T) {
	// 本地开发依赖 configs/config.yaml 的固定密钥（否则每次重启掉线），
	// 只在 production 强制注入。
	cfg := Defaults()
	cfg.Auth.JWTSecret = "dev-only-insecure-secret-change-me-32b"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("dev secret rejected outside production: %v", err)
	}
}

// TestRepoConfigSecretsAreRejected 是「仓库自带可用密钥」这一类问题的回归钉子：
// 任何配置文件里的 jwt_secret 字面量都必须被生产校验拒绝，否则它就是一个公开
// 可用的签名密钥。新增配置示例时请用明显的占位值（CHANGE_ME_…）或 ${VAR}。
func TestRepoConfigSecretsAreRejected(t *testing.T) {
	files := []string{
		"../../../configs/config.yaml",
		"../../../configs/config.example.yaml",
		"../../../.env.example",
		"../../../deploy/docker-compose.yml",
	}
	// 两种形态都要抓：`jwt_secret: <literal>` 与 `${APP_AUTH_JWT_SECRET:-<literal>}`
	// （compose 的内置默认值就是后一种，它同样是公开可用的密钥）。
	re := regexp.MustCompile(`(?i)(?:APP_AUTH_JWT_SECRET|jwt_secret)[^\n:]*[:=]\s*["']?([A-Za-z0-9+/=_.-]{8,})`)
	reDefault := regexp.MustCompile(`\$\{APP_AUTH_JWT_SECRET:-([^}]+)\}`)
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue // 注释里的示例值不会被加载
			}
			var value string
			// 先试 ${VAR:-default}：否则上面的通用正则会把 ":-" 的 "-" 当成值的一部分。
			if m := reDefault.FindStringSubmatch(line); m != nil {
				value = m[1]
			} else if m := re.FindStringSubmatch(line); m != nil {
				value = m[1]
			} else {
				continue
			}
			if !isPlaceholderSecret(value) {
				t.Errorf("%s:%d 携带可用 jwt 密钥 %q；请改成明显占位值（CHANGE_ME_…）或 ${APP_AUTH_JWT_SECRET}", path, i+1, value)
			}
		}
	}
}

func TestTrustedProxiesFromEnv(t *testing.T) {
	t.Setenv("APP_HTTP_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.10")

	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.HTTPServer.TrustedProxies) != 2 {
		t.Fatalf("trusted proxies = %v, want 2 entries", cfg.HTTPServer.TrustedProxies)
	}
	if cfg.HTTPServer.TrustedProxies[0] != "10.0.0.0/8" || cfg.HTTPServer.TrustedProxies[1] != "192.168.1.10" {
		t.Errorf("trusted proxies = %v, want [10.0.0.0/8 192.168.1.10]", cfg.HTTPServer.TrustedProxies)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid trusted proxies rejected: %v", err)
	}
}

func TestValidateRejectsMalformedTrustedProxy(t *testing.T) {
	cfg := Defaults()
	cfg.HTTPServer.TrustedProxies = []string{"10.0.0.0/8", "not-an-ip"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for a malformed trusted proxy entry")
	}
}

func TestTrustedProxiesDefaultToNone(t *testing.T) {
	// 默认必须「不信任任何代理」：X-Forwarded-For 由客户端随意填写。
	cfg := Defaults()
	if len(cfg.HTTPServer.TrustedProxies) != 0 {
		t.Errorf("default trusted proxies = %v, want none", cfg.HTTPServer.TrustedProxies)
	}
}

func TestTrustedProxiesEmptyEnvIsNoProxy(t *testing.T) {
	// 空字符串必须等同于「未设置」，否则会变成 [""] 并在校验阶段炸掉启动。
	t.Setenv("APP_HTTP_TRUSTED_PROXIES", "")
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load with empty env: %v", err)
	}
	if len(cfg.HTTPServer.TrustedProxies) != 0 {
		t.Errorf("trusted proxies = %v, want none", cfg.HTTPServer.TrustedProxies)
	}
}

func TestMetricsEnabledDefaultAndOverride(t *testing.T) {
	if !Defaults().Metrics.Enabled {
		t.Error("metrics.enabled should default to true")
	}
	t.Setenv("APP_METRICS_ENABLED", "false")
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Metrics.Enabled {
		t.Error("APP_METRICS_ENABLED=false should disable the endpoint")
	}
}

func TestRateLimitBackendDefaultsToMemory(t *testing.T) {
	cfg := Defaults()
	if cfg.RateLimit.Backend != "memory" {
		t.Errorf("ratelimit.backend = %q, want memory", cfg.RateLimit.Backend)
	}
	if cfg.Redis.Addr != "" {
		t.Errorf("redis.addr = %q, want empty by default", cfg.Redis.Addr)
	}
}

func TestValidateRejectsRedisBackendWithoutAddr(t *testing.T) {
	// 配了 redis 后端却没给地址：必须 fail-fast，否则多副本会静默退化成
	// 「每个进程一份额度」，而运维以为集群共享桶。
	cfg := Defaults()
	cfg.RateLimit.Backend = "redis"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted ratelimit.backend=redis without redis.addr")
	}
	if !strings.Contains(err.Error(), "redis.addr") {
		t.Errorf("error %q does not point at redis.addr", err)
	}

	cfg.Redis.Addr = "127.0.0.1:6379"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with redis.addr: %v", err)
	}
}

func TestValidateRejectsUnknownRateLimitBackend(t *testing.T) {
	cfg := Defaults()
	cfg.RateLimit.Backend = "memcached"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted an unknown ratelimit backend")
	}
	if !strings.Contains(err.Error(), "ratelimit.backend") {
		t.Errorf("error %q does not mention ratelimit.backend", err)
	}
}

func TestRedisSettingsFromEnv(t *testing.T) {
	t.Setenv("APP_RATELIMIT_BACKEND", "redis")
	t.Setenv("APP_REDIS_ADDR", "redis.internal:6379")
	t.Setenv("APP_REDIS_DB", "3")

	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RateLimit.Backend != "redis" {
		t.Errorf("ratelimit.backend = %q, want redis", cfg.RateLimit.Backend)
	}
	if cfg.Redis.Addr != "redis.internal:6379" {
		t.Errorf("redis.addr = %q", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 3 {
		t.Errorf("redis.db = %d, want 3", cfg.Redis.DB)
	}
}

// TestEmptyJWTSecretEnvDoesNotFallBackToTheRepoDefault 钉住「忘记注入密钥」的真实现场：
// compose 的 `${APP_AUTH_JWT_SECRET:-}` 展开成空串时，viper 默认会忽略空环境变量，
// 于是应用拿着 configs/config.yaml 里的开发占位值跑到 production 校验，报出
// 「still uses a template placeholder」——听起来像代码 bug，其实是没设置密钥。
func TestEmptyJWTSecretEnvDoesNotFallBackToTheRepoDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "app:\n  env: production\nauth:\n  jwt_secret: dev-only-insecure-secret-change-me-32b\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// 环境变量未设置：仓库默认值照常生效 → 命中占位值拒绝，并说明去哪里设。
	if _, err := Load(path, "APP"); err == nil {
		t.Fatal("Load accepted the repository placeholder in production")
	} else if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("unset env error = %v, want the placeholder rejection", err)
	}

	// 显式空值（`APP_AUTH_JWT_SECRET=` / compose 的 ${VAR:-}）：不能被当成「未设置」
	// 而回落到仓库默认值，必须直接说「密钥太短/没设置」。
	t.Setenv("APP_AUTH_JWT_SECRET", "")
	_, err := Load(path, "APP")
	if err == nil {
		t.Fatal("Load accepted an empty APP_AUTH_JWT_SECRET in production")
	}
	if strings.Contains(err.Error(), "placeholder") {
		t.Errorf("error = %v, want the missing-secret message, not the placeholder one", err)
	}
	if !strings.Contains(err.Error(), "APP_AUTH_JWT_SECRET") || !strings.Contains(err.Error(), "make docker-up") {
		t.Errorf("error = %v, want it to name APP_AUTH_JWT_SECRET and `make docker-up`", err)
	}
}

// TestJWTKeyRingFromEnv pins the rotation knobs: the kid is trimmed, a
// comma-separated retired-secret list is split and trimmed, and blank entries
// disappear instead of becoming a key of length zero.
func TestJWTKeyRingFromEnv(t *testing.T) {
	active := strings.Repeat("a", minJWTSecretBytes)
	retiredA := strings.Repeat("b", minJWTSecretBytes)
	retiredB := strings.Repeat("c", minJWTSecretBytes)
	t.Setenv("APP_AUTH_JWT_SECRET", active)
	t.Setenv("APP_AUTH_JWT_KEY_ID", "  k2  ")
	t.Setenv("APP_AUTH_JWT_RETIRED_SECRETS", " "+retiredA+" , ,"+retiredB+" ")

	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.JWTKeyID != "k2" {
		t.Errorf("jwt_key_id = %q, want k2", cfg.Auth.JWTKeyID)
	}
	want := []string{retiredA, retiredB}
	if len(cfg.Auth.JWTRetiredSecrets) != len(want) {
		t.Fatalf("retired secrets = %v, want %v", cfg.Auth.JWTRetiredSecrets, want)
	}
	for i, secret := range want {
		if cfg.Auth.JWTRetiredSecrets[i] != secret {
			t.Errorf("retired[%d] = %q, want %q", i, cfg.Auth.JWTRetiredSecrets[i], secret)
		}
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid key ring rejected: %v", err)
	}
}

func TestRetiredSecretsDefaultToEmpty(t *testing.T) {
	t.Setenv("APP_AUTH_JWT_RETIRED_SECRETS", "")
	if cfg := Defaults(); len(cfg.Auth.JWTRetiredSecrets) != 0 {
		t.Errorf("default retired secrets = %v, want none", cfg.Auth.JWTRetiredSecrets)
	}
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load with empty env: %v", err)
	}
	if len(cfg.Auth.JWTRetiredSecrets) != 0 {
		t.Errorf("retired secrets = %v, want none", cfg.Auth.JWTRetiredSecrets)
	}
}

// TestValidateRejectsBadJWTKeyRing covers the rotation footguns: a weak or
// placeholder retired key still verifies access tokens, and re-listing the
// active secret (a copy/paste during a rotation) widens the accepted set for
// no reason.
func TestValidateRejectsBadJWTKeyRing(t *testing.T) {
	active := "Qk3f9vT2zR7pLmW1sXc8nB4hYd6gJa0eUq5rItOy"
	cases := []struct {
		name   string
		secret string
		mutate func(*Config)
		want   string
	}{
		{
			name:   "short retired key in production",
			secret: active,
			mutate: func(cfg *Config) { cfg.Auth.JWTRetiredSecrets = []string{"too-short"} },
			want:   "at least",
		},
		{
			name:   "placeholder retired key in production",
			secret: active,
			mutate: func(cfg *Config) {
				cfg.Auth.JWTRetiredSecrets = []string{"dev-only-insecure-secret-change-me-32b"}
			},
			want: "placeholder",
		},
		{
			name:   "retired key repeats the active secret",
			secret: active,
			mutate: func(cfg *Config) { cfg.Auth.JWTRetiredSecrets = []string{active} },
			want:   "repeats",
		},
		{
			name:   "duplicate retired key",
			secret: active,
			mutate: func(cfg *Config) {
				cfg.Auth.JWTRetiredSecrets = []string{
					"r1-secret-that-is-long-enough-for-hs256",
					"r1-secret-that-is-long-enough-for-hs256",
				}
			},
			want: "duplicate",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.App.Env = "production"
			cfg.Auth.JWTSecret = tc.secret
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("%s: validation accepted the key ring", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestValidateAllowsWeakRetiredKeyOutsideProduction keeps local development
// usable: only production enforces the 32-byte floor.
func TestValidateAllowsWeakRetiredKeyOutsideProduction(t *testing.T) {
	cfg := Defaults()
	cfg.Auth.JWTSecret = "dev-only-insecure-secret-change-me-32b"
	cfg.Auth.JWTRetiredSecrets = []string{"old-dev-secret"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("development rejected a weak retired key: %v", err)
	}
}

// TestVerifyCacheTTLDefaultAndBounds pins the knob that trades a DB read per
// authenticated request for a bounded revocation lag on other replicas.
func TestVerifyCacheTTLDefaultAndBounds(t *testing.T) {
	if got := Defaults().Auth.VerifyCacheTTL; got != 5*time.Second {
		t.Errorf("default verify_cache_ttl = %v, want 5s", got)
	}
	t.Setenv("APP_AUTH_VERIFY_CACHE_TTL", "0")
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.VerifyCacheTTL != 0 {
		t.Errorf("verify_cache_ttl = %v, want 0 (cache off)", cfg.Auth.VerifyCacheTTL)
	}

	for _, ttl := range []string{"-1s", "90s"} {
		cfg := Defaults()
		d, err := time.ParseDuration(ttl)
		if err != nil {
			t.Fatalf("parse %s: %v", ttl, err)
		}
		cfg.Auth.VerifyCacheTTL = d
		if err := cfg.Validate(); err == nil {
			t.Errorf("verify_cache_ttl=%s must be rejected", ttl)
		}
	}
}

// TestReadHeaderTimeoutDefaultAndBounds pins the slowloris guard: the header
// phase gets its own deadline, and it may not outlive the whole read deadline.
func TestReadHeaderTimeoutDefaultAndBounds(t *testing.T) {
	if got := Defaults().HTTPServer.ReadHeaderTimeout; got != 5*time.Second {
		t.Errorf("default read_header_timeout = %v, want 5s", got)
	}

	for _, ttl := range []string{"0s", "-1s", "11s"} {
		cfg := Defaults()
		d, err := time.ParseDuration(ttl)
		if err != nil {
			t.Fatalf("parse %s: %v", ttl, err)
		}
		cfg.HTTPServer.ReadHeaderTimeout = d
		if err := cfg.Validate(); err == nil {
			t.Errorf("read_header_timeout=%s must be rejected", ttl)
		}
	}

	t.Setenv("APP_HTTP_READ_HEADER_TIMEOUT", "3s")
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPServer.ReadHeaderTimeout != 3*time.Second {
		t.Errorf("read_header_timeout = %v, want 3s", cfg.HTTPServer.ReadHeaderTimeout)
	}
}

// TestMaxHeaderBytesDefaultAndBounds pins the header budget: 0 would silently
// restore stdlib's 1 MiB, which is the half of the slowloris surface that
// read_header_timeout does not cover.
func TestMaxHeaderBytesDefaultAndBounds(t *testing.T) {
	if got := Defaults().HTTPServer.MaxHeaderBytes; got != 16<<10 {
		t.Errorf("default max_header_bytes = %d, want 16KiB", got)
	}

	for _, value := range []int{0, -1, 1<<20 + 1} {
		cfg := Defaults()
		cfg.HTTPServer.MaxHeaderBytes = value
		if err := cfg.Validate(); err == nil {
			t.Errorf("max_header_bytes=%d must be rejected", value)
		}
	}

	t.Setenv("APP_HTTP_MAX_HEADER_BYTES", "8192")
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPServer.MaxHeaderBytes != 8192 {
		t.Errorf("max_header_bytes = %d, want 8192", cfg.HTTPServer.MaxHeaderBytes)
	}
}

// TestMessageLeaseMustCoverDeliveryTimeout keeps the two knobs from drifting
// apart: a lease shorter than one send lets another replica reclaim a row that
// is still in flight (the worker renews before sending, but the lease has to
// cover that send).
func TestMessageLeaseMustCoverDeliveryTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.Message.Lease = 30 * time.Second
	cfg.Message.DeliveryTimeout = time.Minute
	if err := cfg.Validate(); err == nil {
		t.Fatal("lease shorter than delivery_timeout must be rejected")
	}

	cfg.Message.Lease = time.Minute
	if err := cfg.Validate(); err != nil {
		t.Errorf("lease == delivery_timeout must be accepted: %v", err)
	}
}

// TestValidateCORS pins the two origin lists that a browser would silently
// reject: a wildcard (explicit "*" or the empty-list fallback) together with
// credentialed requests, and wildcard patterns that gin-contrib/cors never
// expands. Both used to start fine and then break the refresh-cookie flow at
// runtime, where the server only sees 200s and the browser drops the response.
func TestValidateCORS(t *testing.T) {
	tests := []struct {
		name        string
		origins     []string
		credentials bool
		wantErr     string
	}{
		{name: "wildcard without credentials", origins: []string{"*"}, wantErr: ""},
		{name: "wildcard with credentials", origins: []string{"*"}, credentials: true, wantErr: "allow_credentials"},
		{name: "empty list with credentials", origins: nil, credentials: true, wantErr: "allow_credentials"},
		{name: "wildcard pattern with credentials", origins: []string{"https://*.example.com"}, credentials: true, wantErr: "wildcard pattern"},
		{name: "wildcard pattern without credentials", origins: []string{"https://*.example.com"}, wantErr: "wildcard pattern"},
		{name: "origin without scheme", origins: []string{"app.example.com"}, wantErr: "absolute http(s)"},
		{name: "origin with path", origins: []string{"https://app.example.com/admin"}, wantErr: "origin only"},
		{name: "origin with query", origins: []string{"https://app.example.com?x=1"}, wantErr: "origin only"},
		{name: "non-http scheme", origins: []string{"ftp://app.example.com"}, wantErr: "absolute http(s)"},
		{name: "explicit origins with credentials", origins: []string{"https://app.example.com", "http://localhost:5173"}, credentials: true},
		{name: "trailing slash is an origin", origins: []string{"https://app.example.com/"}, credentials: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.CORS.AllowedOrigins = tc.origins
			cfg.CORS.AllowCredentials = tc.credentials

			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestCORSOriginsAreTrimmed covers the comma-separated environment form:
// "https://a, https://b" leaves a leading space on the second entry, which
// neither matches the browser Origin nor passes gin's scheme check.
func TestCORSOriginsAreTrimmed(t *testing.T) {
	t.Setenv("APP_CORS_ALLOWED_ORIGINS", "https://a.example.com, https://b.example.com")
	t.Setenv("APP_CORS_ALLOW_CREDENTIALS", "true")

	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"https://a.example.com", "https://b.example.com"}
	if len(cfg.CORS.AllowedOrigins) != len(want) {
		t.Fatalf("allowed_origins = %q, want %q", cfg.CORS.AllowedOrigins, want)
	}
	for i := range want {
		if cfg.CORS.AllowedOrigins[i] != want[i] {
			t.Fatalf("allowed_origins[%d] = %q, want %q", i, cfg.CORS.AllowedOrigins[i], want[i])
		}
	}
}
