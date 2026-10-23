package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// setDefaults registers every configuration key with its built-in default
// value. Registering all keys is what makes environment overrides work for
// nested fields (APP_DATABASE_HOST → database.host).
func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "go-web-template")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.debug", true)
	v.SetDefault("app.port", 8080)
	v.SetDefault("app.graceful_timeout", "10s")

	v.SetDefault("http.read_timeout", "10s")
	v.SetDefault("http.write_timeout", "10s")
	v.SetDefault("http.idle_timeout", "60s")
	v.SetDefault("http.shutdown_timeout", "10s")
	v.SetDefault("http.request_timeout", "15s")

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

	v.SetDefault("swagger.enabled", true)
}

// Defaults returns the built-in default configuration.
func Defaults() *Config {
	v := viper.New()
	setDefaults(v)
	cfg := &Config{}
	_ = v.Unmarshal(cfg) // defaults are always valid
	return cfg
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
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// bindAppEnv maps the top-level "app" section to APP_<KEY> (APP_PORT,
// APP_ENV, ...) instead of the doubled APP_APP_<KEY> that AutomaticEnv would
// derive from the "app.port" key. Other sections keep the natural
// APP_<SECTION>_<KEY> form (APP_DATABASE_HOST, APP_LOG_LEVEL, ...).
func bindAppEnv(v *viper.Viper, prefix string) error {
	keys := []string{"name", "env", "debug", "port", "graceful_timeout"}
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
	if c.HTTPServer.ReadTimeout <= 0 || c.HTTPServer.WriteTimeout <= 0 {
		return fmt.Errorf("http read/write timeouts must be positive")
	}
	if c.HTTPServer.RequestTimeout <= 0 {
		return fmt.Errorf("http.request_timeout must be positive")
	}
	return nil
}
