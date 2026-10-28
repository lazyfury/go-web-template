package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// minJWTSecretBytes is the minimum HS256 signing key length recommended by
// RFC 7518 §3.2 (256 bits).
const minJWTSecretBytes = 32

// setDefaults registers every configuration key with its built-in default
// value. Registering all keys is what makes environment overrides work for
// nested fields (APP_DATABASE_HOST → database.host).
func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "go-web-template")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.debug", true)
	v.SetDefault("app.port", 8080)

	v.SetDefault("http.read_timeout", "10s")
	// 单独的 header 阶段超时：不给 slowloris 一点一点发头把连接挂满 read_timeout 的机会。
	v.SetDefault("http.read_header_timeout", "5s")
	v.SetDefault("http.write_timeout", "10s")
	v.SetDefault("http.idle_timeout", "60s")
	v.SetDefault("http.shutdown_timeout", "10s")
	v.SetDefault("http.request_timeout", "15s")
	v.SetDefault("http.max_body_bytes", 1<<20)
	// Header 上限：访问日志/追踪头 + refresh cookie 远不到 16 KiB；stdlib 默认
	// 1 MiB 只是让每个半开连接多占内存（slowloris 的另一半）。
	v.SetDefault("http.max_header_bytes", 16<<10)
	// 默认不信任任何代理：X-Forwarded-For 由客户端随意填写，信任它等于让调用方
	// 自选限流桶并伪造访问日志里的 IP。反向代理部署时显式配置 CIDR。
	v.SetDefault("http.trusted_proxies", []string{})

	v.SetDefault("database.driver", "postgres")
	v.SetDefault("database.host", "127.0.0.1")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "postgres")
	v.SetDefault("database.password", "postgres")
	v.SetDefault("database.dbname", "go_web_template")
	v.SetDefault("database.sslmode", "disable")
	v.SetDefault("database.max_open_conns", 20)
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.conn_max_lifetime", "1h")
	v.SetDefault("database.log_level", "warn")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	v.SetDefault("cors.allowed_origins", []string{"*"})
	v.SetDefault("cors.allowed_methods", []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allowed_headers", []string{"Content-Type", "Authorization", "X-Request-ID"})
	v.SetDefault("cors.exposed_headers", []string{"X-Request-ID"})
	v.SetDefault("cors.allow_credentials", false)
	v.SetDefault("cors.max_age", "12h")

	v.SetDefault("ratelimit.enabled", true)
	v.SetDefault("ratelimit.rps", 100.0)
	v.SetDefault("ratelimit.burst", 200)
	// 默认单实例内存桶；多副本部署必须切 redis（否则每个副本各有一份额度）。
	v.SetDefault("ratelimit.backend", "memory")

	v.SetDefault("redis.addr", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 10)
	v.SetDefault("redis.dial_timeout", "2s")
	v.SetDefault("redis.ping_timeout", "1s")

	v.SetDefault("swagger.enabled", true)
	// 抓取端点默认开启（本地/内网 Prometheus 直接可用）；编排与公网部署在
	// compose/env 里关掉，或用反代 ACL 限制来源。
	v.SetDefault("metrics.enabled", true)

	v.SetDefault("auth.jwt_secret", "")
	// 轮换支持：kid 标在当前密钥上；旧密钥放 jwt_retired_secrets（只验不签），
	// 轮换后旧 access token 自然过期即可。
	v.SetDefault("auth.jwt_key_id", "")
	v.SetDefault("auth.jwt_retired_secrets", []string{})
	v.SetDefault("auth.issuer", "go-web-template")
	v.SetDefault("auth.access_ttl", "15m")
	v.SetDefault("auth.refresh_ttl", "720h")
	v.SetDefault("auth.refresh_max_lifetime", "2160h")
	v.SetDefault("auth.refresh_reuse_interval", "30s")
	v.SetDefault("auth.bcrypt_cost", 12)
	// 每次认证请求都要读用户的 status/role/token_version；短 TTL 缓存省掉这次读。
	// 本进程内的写（改密/改角色/禁用/删除）都会立即失效，跨副本最多滞后一个 TTL。
	// 置 0 关闭缓存（每请求一次 DB 读）。
	v.SetDefault("auth.verify_cache_ttl", "5s")
	v.SetDefault("auth.login_max_attempts", 5)
	v.SetDefault("auth.login_lockout", "15m")
	v.SetDefault("auth.login_rps", 5.0)
	v.SetDefault("auth.login_burst", 10)
	// 改密要跑 bcrypt：按用户限流（突发 5 次，之后每 5 秒 1 次）。
	v.SetDefault("auth.password_rps", 0.2)
	v.SetDefault("auth.password_burst", 5)
	v.SetDefault("auth.cookie_secure", true)

	v.SetDefault("message.worker_enabled", true)
	v.SetDefault("message.poll_interval", "5s")
	v.SetDefault("message.batch_size", 50)
	v.SetDefault("message.lease", "2m")
	v.SetDefault("message.retry_base", "30s")
	v.SetDefault("message.delivery_timeout", "30s")
	v.SetDefault("message.webhook_urls", []string{})
	v.SetDefault("message.webhook_secret", "")
	v.SetDefault("message.webhook_allow_private", false)
	v.SetDefault("message.mail.host", "")
	v.SetDefault("message.mail.port", 587)
	v.SetDefault("message.mail.username", "")
	v.SetDefault("message.mail.password", "")
	v.SetDefault("message.mail.from", "")
	v.SetDefault("message.sms.provider", "")
	v.SetDefault("message.sms.account_sid", "")
	v.SetDefault("message.sms.auth_token", "")
	v.SetDefault("message.sms.from", "")
	v.SetDefault("message.sms.endpoint", "")
	v.SetDefault("message.default_locale", "zh-CN")
	v.SetDefault("message.max_attempts", 5)

	// 保留期清理：默认开启，但都留了宽限期（过期后再留一段），避免刚过期的数据
	// 立刻消失，排查/对账还有窗口。关闭后表会无界增长。
	v.SetDefault("retention.enabled", true)
	v.SetDefault("retention.interval", "1h")
	v.SetDefault("retention.timeout", "1m")
	v.SetDefault("retention.refresh_token_retention", "168h")
	v.SetDefault("retention.delivery_retention", "720h")
	v.SetDefault("retention.message_retention", "2160h")
}

// Defaults returns the built-in default configuration.
func Defaults() *Config {
	v := viper.New()
	setDefaults(v)
	cfg := &Config{}
	_ = v.Unmarshal(cfg) // defaults are always valid
	cfg.normalize()
	return cfg
}

// normalize cleans values whose textual form is human friendly but not machine
// friendly, e.g. APP_HTTP_TRUSTED_PROXIES="10.0.0.0/8, 192.168.1.10" keeps the
// space after the comma. gin parses these entries verbatim, so they must be
// trimmed before they reach SetTrustedProxies.
func (c *Config) normalize() {
	for i, proxy := range c.HTTPServer.TrustedProxies {
		c.HTTPServer.TrustedProxies[i] = strings.TrimSpace(proxy)
	}
	c.Auth.JWTKeyID = strings.TrimSpace(c.Auth.JWTKeyID)
	c.Auth.JWTSecret = strings.TrimSpace(c.Auth.JWTSecret)
	// 列表来自 YAML 或逗号分隔的环境变量，两边都可能有空白/空项。
	retired := make([]string, 0, len(c.Auth.JWTRetiredSecrets))
	for _, secret := range c.Auth.JWTRetiredSecrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			retired = append(retired, secret)
		}
	}
	c.Auth.JWTRetiredSecrets = retired
	// 逗号分隔的环境变量同样带空格：不 trim 的话 "https://a, https://b" 里的第二项
	// 既不匹配浏览器发来的 Origin，也过不了 gin-contrib/cors 的 scheme 校验。
	origins := make([]string, 0, len(c.CORS.AllowedOrigins))
	for _, origin := range c.CORS.AllowedOrigins {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	c.CORS.AllowedOrigins = origins
}

// Load reads the configuration from a YAML file (optional) and environment
// variables.
//
//   - configFile: explicit path to a YAML file. If empty, "configs/config.yaml"
//     is looked up; a missing file is not an error when env vars provide
//     everything needed.
//   - envPrefix: prefix for environment overrides, e.g. "APP" maps
//     APP_DATABASE_HOST → database.host.
func Load(configFile, envPrefix string) (*Config, error) {
	// Load secrets/overrides from a local .env file if present. Errors are
	// ignored: the file is optional.
	_ = godotenv.Load()

	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setDefaults(v)
	if err := bindAppEnv(v, envPrefix); err != nil {
		return nil, err
	}

	v.SetConfigType("yaml")
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath("./configs")
	}
	if err := v.ReadInConfig(); err != nil {
		var nf viper.ConfigFileNotFoundError
		if !errors.As(err, &nf) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// No config file: defaults + env vars only.
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.normalize()
	honourEmptyDangerousEnv(cfg, envPrefix)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// honourEmptyDangerousEnv makes "explicitly empty" win over the value shipped in
// configs/config.yaml for the one key where it matters.
//
// viper ignores empty environment variables by default, so compose's
// `APP_AUTH_JWT_SECRET: "${APP_AUTH_JWT_SECRET:-}"` (an operator who forgot to
// export the secret) silently left the repository's development secret in
// place. Production then failed with "jwt_secret still uses a template
// placeholder", which reads like a bug in the code instead of "you did not set
// the secret". Treating the empty value as empty makes the message say exactly
// that.
func honourEmptyDangerousEnv(cfg *Config, envPrefix string) {
	if raw, ok := os.LookupEnv(envPrefix + "_AUTH_JWT_SECRET"); ok && strings.TrimSpace(raw) == "" {
		cfg.Auth.JWTSecret = ""
	}
}

// bindAppEnv maps the top-level "app" section to APP_<KEY> (APP_PORT,
// APP_ENV, ...) instead of the doubled APP_APP_<KEY> that AutomaticEnv would
// derive from the "app.port" key. Other sections keep the natural
// APP_<SECTION>_<KEY> form (APP_DATABASE_HOST, APP_LOG_LEVEL, ...).
func bindAppEnv(v *viper.Viper, prefix string) error {
	keys := []string{"name", "env", "debug", "port"}
	for _, key := range keys {
		envName := prefix + "_" + strings.ToUpper(key)
		if err := v.BindEnv("app."+key, envName); err != nil {
			return fmt.Errorf("bind env %s: %w", envName, err)
		}
	}
	return nil
}

// Validate performs semantic checks that cannot be expressed in the schema.
func (c *Config) Validate() error {
	if c.App.Port <= 0 || c.App.Port > 65535 {
		return fmt.Errorf("invalid app.port %d", c.App.Port)
	}
	if c.Database.Driver != "postgres" {
		return fmt.Errorf("unsupported database.driver %q (only postgres)", c.Database.Driver)
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log.level %q", c.Log.Level)
	}
	switch c.Log.Format {
	case "json", "console":
	default:
		return fmt.Errorf("invalid log.format %q", c.Log.Format)
	}
	if c.RateLimit.Enabled && c.RateLimit.RPS <= 0 {
		return fmt.Errorf("ratelimit.rps must be positive")
	}
	switch c.RateLimit.Backend {
	case "memory":
	case "redis":
		if c.Redis.Addr == "" {
			return fmt.Errorf("ratelimit.backend=redis requires redis.addr (set APP_REDIS_ADDR)")
		}
		if c.Redis.DialTimeout <= 0 || c.Redis.PingTimeout <= 0 {
			return fmt.Errorf("redis.dial_timeout and redis.ping_timeout must be positive")
		}
		if c.Redis.PoolSize <= 0 {
			return fmt.Errorf("redis.pool_size must be positive")
		}
	default:
		return fmt.Errorf("invalid ratelimit.backend %q (memory|redis)", c.RateLimit.Backend)
	}
	if c.HTTPServer.ReadTimeout <= 0 || c.HTTPServer.WriteTimeout <= 0 {
		return fmt.Errorf("http read/write timeouts must be positive")
	}
	if c.HTTPServer.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("http.read_header_timeout must be positive")
	}
	if c.HTTPServer.ReadHeaderTimeout > c.HTTPServer.ReadTimeout {
		return fmt.Errorf("http.read_header_timeout must not exceed http.read_timeout")
	}
	if c.HTTPServer.RequestTimeout <= 0 {
		return fmt.Errorf("http.request_timeout must be positive")
	}
	if c.HTTPServer.MaxBodyBytes <= 0 {
		return fmt.Errorf("http.max_body_bytes must be positive")
	}
	// 0 would silently fall back to stdlib's 1 MiB: require an explicit budget.
	if c.HTTPServer.MaxHeaderBytes <= 0 || c.HTTPServer.MaxHeaderBytes > 1<<20 {
		return fmt.Errorf("http.max_header_bytes must be between 1 and 1048576")
	}
	for _, proxy := range c.HTTPServer.TrustedProxies {
		entry := strings.TrimSpace(proxy)
		if net.ParseIP(entry) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(entry); err != nil {
			return fmt.Errorf("http.trusted_proxies entry %q is neither an IP nor a CIDR", proxy)
		}
	}
	if err := c.validateCORS(); err != nil {
		return err
	}
	if c.Message.MaxAttempts <= 0 {
		return fmt.Errorf("message.max_attempts must be positive")
	}
	if c.Message.WorkerEnabled {
		if c.Message.PollInterval <= 0 || c.Message.Lease <= 0 || c.Message.RetryBase <= 0 || c.Message.DeliveryTimeout <= 0 {
			return fmt.Errorf("message worker durations must be positive")
		}
		if c.Message.BatchSize <= 0 {
			return fmt.Errorf("message.batch_size must be positive")
		}
		// A lease shorter than one send means the row can be reclaimed while it
		// is still in flight; the worker renews before sending, but the lease
		// must still cover a whole delivery attempt.
		if c.Message.Lease < c.Message.DeliveryTimeout {
			return fmt.Errorf("message.lease must be at least message.delivery_timeout")
		}
	}
	if c.Message.Mail.Host != "" && c.Message.Mail.From == "" {
		return fmt.Errorf("message.mail.from is required when mail.host is set")
	}
	if c.Retention.Enabled {
		if c.Retention.Interval <= 0 {
			return fmt.Errorf("retention.interval must be positive")
		}
		if c.Retention.Timeout <= 0 {
			return fmt.Errorf("retention.timeout must be positive")
		}
		if c.Retention.RefreshTokenRetention < 0 || c.Retention.DeliveryRetention < 0 || c.Retention.MessageRetention < 0 {
			return fmt.Errorf("retention windows must not be negative")
		}
	}
	return c.validateAuth()
}

// validateCORS rejects origin lists that cannot work in a browser, so the
// failure is a startup error with a readable message instead of a silent CORS
// failure (or a panic inside gin-contrib/cors, which validates the same field
// by panicking in cors.New).
//
// The credentialed case matters most: the admin SPA relies on the HttpOnly
// refresh cookie, and browsers reject `Access-Control-Allow-Origin: *` together
// with `Access-Control-Allow-Credentials: true`. A deployment that enables
// cors.allow_credentials but leaves the shipped `*` default would therefore
// log 200s while every refresh from the browser fails.
func (c *Config) validateCORS() error {
	if len(c.CORS.AllowedOrigins) == 0 && c.CORS.AllowCredentials {
		// web.CORS falls back to "*" for an empty list, which is the same
		// credentialed-wildcard trap as an explicit "*".
		return fmt.Errorf("cors.allowed_origins must be set explicitly while cors.allow_credentials is true (an empty list falls back to \"*\", which browsers reject on credentialed requests)")
	}
	for i, origin := range c.CORS.AllowedOrigins {
		switch {
		case origin == "":
			return fmt.Errorf("cors.allowed_origins[%d] is empty", i)
		case origin == "*":
			if c.CORS.AllowCredentials {
				return fmt.Errorf("cors.allowed_origins cannot be \"*\" while cors.allow_credentials is true: browsers reject a wildcard origin on credentialed requests (the refresh-cookie flow stops working); list the frontend origins explicitly")
			}
		case strings.Contains(origin, "*"):
			// gin-contrib/cors only expands wildcard patterns when AllowWildcard is
			// set, and this template does not set it: such an entry would silently
			// never match instead of failing loudly.
			return fmt.Errorf("cors.allowed_origins[%d] %q uses a wildcard pattern, which is not supported: list explicit origins (for example https://app.example.com)", i, origin)
		default:
			u, err := url.Parse(origin)
			if err != nil {
				return fmt.Errorf("cors.allowed_origins[%d] %q is not a valid URL: %w", i, origin, err)
			}
			if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("cors.allowed_origins[%d] %q must be an absolute http(s) origin, for example https://app.example.com", i, origin)
			}
			if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
				return fmt.Errorf("cors.allowed_origins[%d] %q must be an origin only (no path, query, fragment or userinfo)", i, origin)
			}
		}
	}
	return nil
}

// placeholderSecretMarkers lists the families of non-secrets that show up in
// templates, docs and local compose files. Anything matching one of them is
// public knowledge by definition, so signing production tokens with it is the
// same as having no authentication at all: anyone who can read the repo can
// mint an admin token.
var placeholderSecretMarkers = []string{
	"change_me", "change-me", "changeme",
	"insecure",
	"placeholder",
	"example",
	"local-compose-default",
	"dev-only",
	"replace-with",
	"your-secret",
}

// isPlaceholderSecret rejects secrets that are public knowledge: any literal
// shipped inside this repository (configs/config.yaml, .env.example and the
// default that used to live in deploy/docker-compose.yml) or an obvious
// template marker. A production secret must be random and injected from
// outside the repository.
func isPlaceholderSecret(secret string) bool {
	lower := strings.ToLower(secret)
	for _, marker := range placeholderSecretMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// validateAuth enforces the non-negotiable authentication invariants: a strong
// signing secret in production (fail-fast instead of shipping a guessable
// token), positive lifetimes and a sane bcrypt cost.
func (c *Config) validateAuth() error {
	if c.IsProduction() {
		if len(c.Auth.JWTSecret) < minJWTSecretBytes {
			return fmt.Errorf("auth.jwt_secret must be at least %d bytes in production: set APP_AUTH_JWT_SECRET, or run `make docker-up` (it generates .local/jwt-secret and injects it)", minJWTSecretBytes)
		}
		if isPlaceholderSecret(c.Auth.JWTSecret) {
			return fmt.Errorf("auth.jwt_secret still uses a placeholder shipped in this repository (configs/config.yaml or an example value): set APP_AUTH_JWT_SECRET to a random value of at least %d bytes; `make docker-up` generates and injects .local/jwt-secret", minJWTSecretBytes)
		}
	}
	if c.Auth.Issuer == "" {
		return fmt.Errorf("auth.issuer must not be empty")
	}
	if err := c.validateJWTKeys(); err != nil {
		return err
	}
	if c.Auth.AccessTTL <= 0 {
		return fmt.Errorf("auth.access_ttl must be positive")
	}
	if c.Auth.RefreshTTL <= c.Auth.AccessTTL {
		return fmt.Errorf("auth.refresh_ttl must be greater than auth.access_ttl")
	}
	if c.Auth.RefreshMaxLifetime < c.Auth.RefreshTTL {
		return fmt.Errorf("auth.refresh_max_lifetime must be >= auth.refresh_ttl")
	}
	if c.Auth.RefreshReuseInterval < 0 || c.Auth.RefreshReuseInterval > time.Minute {
		return fmt.Errorf("auth.refresh_reuse_interval must be between 0 and 1m")
	}
	if c.Auth.BcryptCost < 10 || c.Auth.BcryptCost > 14 {
		return fmt.Errorf("auth.bcrypt_cost must be between 10 and 14")
	}
	if c.Auth.VerifyCacheTTL < 0 || c.Auth.VerifyCacheTTL > time.Minute {
		return fmt.Errorf("auth.verify_cache_ttl must be between 0 and 1m: a longer window delays revocation on other replicas")
	}
	if c.Auth.LoginMaxAttempts <= 0 {
		return fmt.Errorf("auth.login_max_attempts must be positive")
	}
	if c.Auth.LoginLockout <= 0 {
		return fmt.Errorf("auth.login_lockout must be positive")
	}
	if c.Auth.LoginRPS <= 0 || c.Auth.LoginBurst <= 0 {
		return fmt.Errorf("auth.login_rps and auth.login_burst must be positive")
	}
	if c.Auth.PasswordRPS <= 0 || c.Auth.PasswordBurst <= 0 {
		return fmt.Errorf("auth.password_rps and auth.password_burst must be positive")
	}
	return nil
}

// validateJWTKeys enforces the rotation ring's invariants: a retired key is a
// trusted key that can still mint valid tokens, so it may not be weaker than
// the active one, and configuring the active secret twice (a copy/paste during
// rotation) would silently widen the set of accepted tokens.
func (c *Config) validateJWTKeys() error {
	seen := make(map[string]int, len(c.Auth.JWTRetiredSecrets))
	for i, secret := range c.Auth.JWTRetiredSecrets {
		if secret == c.Auth.JWTSecret && c.Auth.JWTSecret != "" {
			return fmt.Errorf("auth.jwt_retired_secrets[%d] repeats auth.jwt_secret: keep only the previous key here", i)
		}
		if _, dup := seen[secret]; dup {
			return fmt.Errorf("auth.jwt_retired_secrets contains a duplicate key")
		}
		seen[secret] = i
		if !c.IsProduction() {
			continue
		}
		if len(secret) < minJWTSecretBytes {
			return fmt.Errorf("auth.jwt_retired_secrets must be at least %d bytes in production: a retired key still verifies access tokens", minJWTSecretBytes)
		}
		if isPlaceholderSecret(secret) {
			return fmt.Errorf("auth.jwt_retired_secrets still uses a placeholder shipped in this repository")
		}
	}
	return nil
}
