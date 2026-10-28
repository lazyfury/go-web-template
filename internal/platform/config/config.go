// Package config defines the application configuration model and its
// loading logic (YAML file + environment variable overrides).
//
// Precedence (low → high): built-in defaults → YAML file → environment.
// Every key is registered as a viper default, so an env var such as
// APP_DATABASE_HOST overrides configs/config.yaml's database.host.
package config

import "time"

// Config is the root application configuration.
type Config struct {
	App        App        `mapstructure:"app"`
	HTTPServer HTTPServer `mapstructure:"http"`
	Database   Database   `mapstructure:"database"`
	Log        Log        `mapstructure:"log"`
	CORS       CORS       `mapstructure:"cors"`
	RateLimit  RateLimit  `mapstructure:"ratelimit"`
	Redis      Redis      `mapstructure:"redis"`
	Swagger    Swagger    `mapstructure:"swagger"`
	Metrics    Metrics    `mapstructure:"metrics"`
	Auth       Auth       `mapstructure:"auth"`
	Message    Message    `mapstructure:"message"`
	Retention  Retention  `mapstructure:"retention"`
}

// App holds generic application metadata.
type App struct {
	Name  string `mapstructure:"name"`
	Env   string `mapstructure:"env"` // development | staging | production
	Debug bool   `mapstructure:"debug"`
	Port  int    `mapstructure:"port"`
}

// HTTPServer holds net/http server timeouts.
type HTTPServer struct {
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
	// ReadHeaderTimeout bounds the request-header phase on its own. ReadTimeout
	// already covers the whole read, but a separate header deadline is what lets
	// the server drop a slowloris client early without shortening the time
	// allowed for a legitimately large body.
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
	// RequestTimeout bounds handler execution via the request context (the
	// /api/v1 chain). Handlers/repositories must honor ctx.
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	// MaxBodyBytes caps the request body size; 0 disables the check.
	MaxBodyBytes int64 `mapstructure:"max_body_bytes"`
	// MaxHeaderBytes caps the request-header size. stdlib defaults to 1 MiB,
	// which is far more than this API's headers ever need: a bigger budget only
	// buys an attacker more per-connection memory before the parser rejects it.
	MaxHeaderBytes int `mapstructure:"max_header_bytes"`
	// TrustedProxies lists the reverse proxies (IP or CIDR) that may set
	// X-Forwarded-For / X-Real-IP. Empty means "trust nobody": the client IP
	// comes from the TCP peer, so a caller cannot choose its own rate-limit
	// bucket nor forge the IP recorded in the access log.
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

// Database holds the PostgreSQL connection settings.
type Database struct {
	Driver          string        `mapstructure:"driver"` // postgres (sqlite is reserved for tests)
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Name            string        `mapstructure:"dbname"`
	SSLMode         string        `mapstructure:"sslmode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	LogLevel        string        `mapstructure:"log_level"` // silent | error | warn | info
	// SlowThreshold is when gorm logs a statement at warn level. Keep it well
	// below the request timeout: a query that is slow but still succeeds is
	// invisible to the access log's status codes.
	SlowThreshold time.Duration `mapstructure:"slow_threshold"`
}

// Log holds the zap logger settings.
type Log struct {
	Level  string `mapstructure:"level"`  // debug | info | warn | error
	Format string `mapstructure:"format"` // json | console
}

// CORS holds the cross-origin resource sharing settings.
type CORS struct {
	AllowedOrigins   []string      `mapstructure:"allowed_origins"`
	AllowedMethods   []string      `mapstructure:"allowed_methods"`
	AllowedHeaders   []string      `mapstructure:"allowed_headers"`
	ExposedHeaders   []string      `mapstructure:"exposed_headers"`
	AllowCredentials bool          `mapstructure:"allow_credentials"`
	MaxAge           time.Duration `mapstructure:"max_age"`
}

// RateLimit holds the per-IP rate limiting settings.
type RateLimit struct {
	Enabled bool    `mapstructure:"enabled"`
	RPS     float64 `mapstructure:"rps"`
	Burst   int     `mapstructure:"burst"`
	// Backend selects where the buckets live: "memory" (per process, correct
	// for one instance) or "redis" (shared, required as soon as there is more
	// than one replica — otherwise each replica gets its own allowance).
	Backend string `mapstructure:"backend"`
}

// Redis holds the optional Redis connection used for cross-instance state.
// Redis is never a source of truth: the database stays authoritative.
type Redis struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
	// DialTimeout also bounds reads/writes, so a stalled Redis fails the
	// request quickly instead of holding a handler open.
	DialTimeout time.Duration `mapstructure:"dial_timeout"`
	// PingTimeout bounds the startup connectivity check.
	PingTimeout time.Duration `mapstructure:"ping_timeout"`
}

// Metrics controls the Prometheus scrape endpoint.
type Metrics struct {
	// Enabled mounts GET /metrics. Turn it off when the port is reachable from
	// the public internet and scraping happens through a sidecar or an ACL'd
	// reverse proxy instead: the endpoint advertises the Go version, the full
	// route inventory and the traffic volume.
	Enabled bool `mapstructure:"enabled"`
}

// Swagger controls whether /openapi.json and the Swagger UI are exposed.
type Swagger struct {
	Enabled bool `mapstructure:"enabled"`
}

// Auth holds the authentication and token settings.
type Auth struct {
	// JWTSecret signs access tokens (HMAC-SHA256). Production requires at
	// least 32 bytes; a short or empty secret fails startup.
	JWTSecret string `mapstructure:"jwt_secret"`
	// JWTKeyID is stamped as the JWT "kid" header on newly issued tokens.
	// Optional; empty omits the header. It lets a rotation be traced per token
	// (which key signed it) and gives verification a fast path.
	JWTKeyID string `mapstructure:"jwt_key_id"`
	// JWTRetiredSecrets are verification-only keys: put the previous secret here
	// when rotating jwt_secret so access tokens issued before the switch keep
	// working until they expire. Drop them once access_ttl has passed.
	JWTRetiredSecrets []string `mapstructure:"jwt_retired_secrets"`
	Issuer            string   `mapstructure:"issuer"`

	// AccessTTL bounds the stateless access token lifetime.
	AccessTTL time.Duration `mapstructure:"access_ttl"`
	// RefreshTTL bounds the refresh token lifetime. Rotation slides it, so an
	// actively used client never has to sign in again.
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
	// RefreshMaxLifetime is the absolute cap for one session: after this long
	// (measured from the first login) the user must sign in again even if the
	// token keeps being rotated.
	RefreshMaxLifetime time.Duration `mapstructure:"refresh_max_lifetime"`
	// RefreshReuseInterval tolerates a rotated-away refresh token for a short
	// window (multiple tabs racing, browser retries). Outside the window a
	// reused token is treated as theft and revokes every session.
	RefreshReuseInterval time.Duration `mapstructure:"refresh_reuse_interval"`

	// BcryptCost is the password hashing cost (10-14).
	BcryptCost int `mapstructure:"bcrypt_cost"`

	// VerifyCacheTTL memoizes the per-user status/role/token-version read that
	// every authenticated request needs. In-process writes (password change,
	// role change, disable, delete) invalidate immediately, so the TTL only
	// bounds how long another replica can lag behind. 0 disables the cache.
	VerifyCacheTTL time.Duration `mapstructure:"verify_cache_ttl"`

	// LoginMaxAttempts consecutive failures lock the account for LoginLockout.
	LoginMaxAttempts int           `mapstructure:"login_max_attempts"`
	LoginLockout     time.Duration `mapstructure:"login_lockout"`

	// LoginRPS/LoginBurst throttle POST /auth/login per client IP.
	LoginRPS   float64 `mapstructure:"login_rps"`
	LoginBurst int     `mapstructure:"login_burst"`

	// PasswordRPS/PasswordBurst throttle PATCH /auth/password per user id. The
	// endpoint verifies the current password with bcrypt, so an unlimited
	// caller can guess passwords and burn CPU while holding a stolen token.
	PasswordRPS   float64 `mapstructure:"password_rps"`
	PasswordBurst int     `mapstructure:"password_burst"`

	// CookieSecure marks the refresh cookie Secure; set false for local http.
	CookieSecure bool `mapstructure:"cookie_secure"`
}

// Message holds notification delivery and worker settings.
type Message struct {
	// WorkerEnabled starts the delivery worker from cmd/server.
	WorkerEnabled bool `mapstructure:"worker_enabled"`
	// PollInterval is how often the worker claims due deliveries.
	PollInterval time.Duration `mapstructure:"poll_interval"`
	// BatchSize caps how many deliveries one tick claims.
	BatchSize int `mapstructure:"batch_size"`
	// Lease is how long a claimed delivery stays locked before another worker
	// may reclaim it.
	Lease time.Duration `mapstructure:"lease"`
	// RetryBase is the base delay for exponential backoff.
	RetryBase time.Duration `mapstructure:"retry_base"`
	// DeliveryTimeout bounds one external send.
	DeliveryTimeout time.Duration `mapstructure:"delivery_timeout"`

	// WebhookURLs is the allowlist of endpoints used by the webhook channel.
	// Empty disables the channel.
	WebhookURLs []string `mapstructure:"webhook_urls"`
	// WebhookSecret signs webhook bodies with HMAC-SHA256 when non-empty.
	WebhookSecret string `mapstructure:"webhook_secret"`
	// WebhookAllowPrivate permits loopback/private IP targets. Keep false in
	// production; tests may enable it for httptest servers.
	WebhookAllowPrivate bool `mapstructure:"webhook_allow_private"`

	// Mail configures the SMTP email sender.
	Mail MessageMail `mapstructure:"mail"`
	// SMS configures the SMS provider (currently Twilio).
	SMS MessageSMS `mapstructure:"sms"`

	// DefaultLocale is used when a template lookup has no explicit locale.
	DefaultLocale string `mapstructure:"default_locale"`
	// MaxAttempts is the default retry budget for external deliveries.
	MaxAttempts int `mapstructure:"max_attempts"`
}

// Retention controls the housekeeping janitor: the worker that hard-deletes
// rows which outlived their window (expired refresh tokens, terminal delivery
// records, expired messages). Retention is measured from the moment a row
// becomes prunable — expiry for tokens and messages, last update for terminal
// deliveries — not from creation, so a grace period after expiry is easy to
// reason about.
type Retention struct {
	// Enabled starts the janitor from cmd/server. When disabled nothing is ever
	// deleted automatically; the tables then grow unbounded.
	Enabled bool `mapstructure:"enabled"`
	// Interval is how often every task runs.
	Interval time.Duration `mapstructure:"interval"`
	// Timeout bounds one task so a slow DELETE cannot hold the tick.
	Timeout time.Duration `mapstructure:"timeout"`
	// RefreshTokenRetention keeps expired refresh tokens this long. A revoked or
	// expired token answers "revoked" instead of "unknown" while it exists, which
	// is what the reuse detection relies on for older sessions.
	RefreshTokenRetention time.Duration `mapstructure:"refresh_token_retention"`
	// DeliveryRetention keeps terminal (sent/dead/canceled) delivery rows this
	// long after their last update. Pending and retryable rows are never pruned.
	DeliveryRetention time.Duration `mapstructure:"delivery_retention"`
	// MessageRetention keeps messages this long after their own expires_at.
	// Messages without expires_at never expire and are never pruned.
	MessageRetention time.Duration `mapstructure:"message_retention"`
}

// MessageMail configures the SMTP sender. Empty Host disables email delivery.
type MessageMail struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

// MessageSMS configures the SMS sender. Empty Provider disables SMS delivery.
type MessageSMS struct {
	Provider   string `mapstructure:"provider"` // twilio
	AccountSID string `mapstructure:"account_sid"`
	AuthToken  string `mapstructure:"auth_token"`
	From       string `mapstructure:"from"`
	Endpoint   string `mapstructure:"endpoint"` // override for tests/proxies
}

// IsProduction reports whether the app runs with production hardening rules.
func (c *Config) IsProduction() bool { return c.App.Env == "production" }
