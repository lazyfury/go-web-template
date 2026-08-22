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
}

// App holds generic application metadata.
type App struct {
	Name            string        `mapstructure:"name"`
	Env             string        `mapstructure:"env"` // development | staging | production
	Debug           bool          `mapstructure:"debug"`
	Port            int           `mapstructure:"port"`
	GracefulTimeout time.Duration `mapstructure:"graceful_timeout"`
}

// HTTPServer holds net/http server timeouts.
type HTTPServer struct {
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
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

// Swagger controls whether the Swagger UI is exposed.
type Swagger struct {
	Enabled bool `mapstructure:"enabled"`
}
