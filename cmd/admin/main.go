// Command admin provisions or resets an administrator account.
//
// The template has no public signup: a fresh deployment is bootstrapped by
// running this binary once (or by `make seed-admin`), then logging in through
// POST /api/v1/auth/login.
//
// Usage:
//
//	APP_SEED_PASSWORD='...' go run ./cmd/admin -email admin@example.com -name Admin
//
// The password is read from APP_SEED_PASSWORD (or -password) and is never
// echoed to stdout or the logs.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/app"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/dbx"
	"github.com/example/go-web-template/internal/platform/logx"
)

// minSeedPassword is the same lower bound the API enforces.
const minSeedPassword = 8

func main() {
	var (
		configFile = flag.String("config", "", "path to config file (default: configs/config.yaml)")
		envPrefix  = flag.String("env-prefix", "APP", "environment variable prefix for config overrides")
		email      = flag.String("email", "admin@example.com", "email of the administrator account")
		name       = flag.String("name", "Administrator", "display name")
		role       = flag.String("role", "admin", "role to grant (admin|member)")
		password   = flag.String("password", "", "password (prefer APP_SEED_PASSWORD to keep it out of the process list)")
	)
	flag.Parse()

	passwordValue := *password
	if passwordValue == "" {
		passwordValue = os.Getenv("APP_SEED_PASSWORD")
	}
	if len(passwordValue) < minSeedPassword {
		log.Fatalf("password must be at least %d characters; set APP_SEED_PASSWORD", minSeedPassword)
	}

	cfg, err := config.Load(*configFile, *envPrefix)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	logger, err := logx.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush

	db, err := dbx.New(&cfg.Database, logger)
	if err != nil {
		logger.Fatal("connect database", zap.Error(err))
	}

	seededRole := authkit.Role(*role)
	if !seededRole.Valid() {
		logger.Fatal("invalid role", zap.String("role", *role))
	}

	account, err := app.SeedAdmin(cfg, logger, db, app.SeedOptions{
		Name:     *name,
		Email:    *email,
		Password: passwordValue,
		Role:     seededRole,
	})
	if err != nil {
		logger.Fatal("seed admin", zap.Error(err))
	}
	fmt.Printf("seeded %s account: id=%d email=%s\n", account.Role, account.ID, account.Email)
}
