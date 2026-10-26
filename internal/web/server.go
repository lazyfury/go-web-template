package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/config"
)

// Server wraps an http.Server with graceful-shutdown handling.
type Server struct {
	http            *http.Server
	shutdownTimeout time.Duration
	logger          *zap.Logger
}

// NewServer creates a Server from an address, the HTTP timeout settings and a
// handler. It deliberately takes the narrow HTTPServer config (not the whole
// config tree) so the lifecycle only depends on what it uses; the pointer keeps
// the signature cheap as that struct grows.
func NewServer(addr string, cfg *config.HTTPServer, handler http.Handler, logger *zap.Logger) *Server {
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
	return &Server{
		http:            httpServer,
		shutdownTimeout: cfg.ShutdownTimeout,
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
