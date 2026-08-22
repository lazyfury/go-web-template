// Package server owns the lifecycle of the net/http server.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/config"
)

// Server wraps an http.Server with graceful-shutdown handling.
type Server struct {
	http            *http.Server
	shutdownTimeout time.Duration
	logger          *zap.Logger
}

// New creates a Server from a gin engine (http.Handler).
func New(cfg *config.Config, handler http.Handler, logger *zap.Logger) *Server {
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.App.Port),
		Handler:      handler,
		ReadTimeout:  cfg.HTTPServer.ReadTimeout,
		WriteTimeout: cfg.HTTPServer.WriteTimeout,
		IdleTimeout:  cfg.HTTPServer.IdleTimeout,
	}
	return &Server{
		http:            httpServer,
		shutdownTimeout: cfg.HTTPServer.ShutdownTimeout,
		logger:          logger,
	}
}

// Run blocks serving HTTP until the context is canceled, then performs a
// graceful shutdown bounded by the configured timeout.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", zap.String("addr", s.http.Addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}
