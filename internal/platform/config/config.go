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
	Swagger    Swagger    `mapstructure:"swagger"`
	Auth       Auth       `mapstructure:"auth"`
	Message    Message    `mapstructure:"message"`
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
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// RequestTimeout bounds handler execution via the request context (the
	// /api/v1 chain). Handlers/repositories must honor ctx.
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	// MaxBodyBytes caps the request body size; 0 disables the check.
	MaxBodyBytes int64 `mapstructure:"max_body_bytes"`
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
	Issuer    string `mapstructure:"issuer"`

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

	// LoginMaxAttempts consecutive failures lock the account for LoginLockout.
	LoginMaxAttempts int           `mapstructure:"login_max_attempts"`
	LoginLockout     time.Duration `mapstructure:"login_lockout"`

	// LoginRPS/LoginBurst throttle POST /auth/login per client IP.
	LoginRPS   float64 `mapstructure:"login_rps"`
	LoginBurst int     `mapstructure:"login_burst"`

	// CookieSecure marks the refresh cookie Secure; set false for local http.
	CookieSecure bool `mapstructure:"cookie_secure"`
}

// Message holds notification delivery settings (Phase 2: no worker yet).
type Message struct {
	// WebhookURLs is the allowlist of endpoints used by the webhook channel.
	// Empty disables the channel.
	WebhookURLs []string `mapstructure:"webhook_urls"`
	// DefaultLocale is used when a template lookup has no explicit locale.
	DefaultLocale string `mapstructure:"default_locale"`
	// MaxAttempts is the default retry budget for external deliveries.
	MaxAttempts int `mapstructure:"max_attempts"`
}

// IsProduction reports whether the app runs with production hardening rules.
func (c *Config) IsProduction() bool { return c.App.Env == "production" }
