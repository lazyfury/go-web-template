package web

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/config"
)

// TestNewServerWiresTimeouts pins the server knobs to the config. A silently
// dropped ReadHeaderTimeout would reopen the slowloris path that
// http.read_header_timeout exists to close, and nothing else would notice.
func TestNewServerWiresTimeouts(t *testing.T) {
	cfg := config.HTTPServer{
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 4 * time.Second,
		WriteTimeout:      11 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   9 * time.Second,
	}
	server := NewServer("127.0.0.1:0", &cfg, http.NotFoundHandler(), zap.NewNop())

	if got := server.http.ReadHeaderTimeout; got != cfg.ReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", got, cfg.ReadHeaderTimeout)
	}
	if got := server.http.ReadTimeout; got != cfg.ReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", got, cfg.ReadTimeout)
	}
	if got := server.http.WriteTimeout; got != cfg.WriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", got, cfg.WriteTimeout)
	}
	if got := server.http.IdleTimeout; got != cfg.IdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", got, cfg.IdleTimeout)
	}
	if server.shutdownTimeout != cfg.ShutdownTimeout {
		t.Errorf("shutdownTimeout = %v, want %v", server.shutdownTimeout, cfg.ShutdownTimeout)
	}
	if server.http.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q", server.http.Addr)
	}
}

func testServerConfig() config.HTTPServer {
	return config.HTTPServer{
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   2 * time.Second,
	}
}

// TestServerRunReportsListenError pins that a bad address fails the process
// instead of being swallowed (cmd/server exits non-zero on a Run error).
func TestServerRunReportsListenError(t *testing.T) {
	cfg := testServerConfig()
	server := NewServer("256.256.256.256:0", &cfg, http.NotFoundHandler(), zap.NewNop())
	if err := server.Run(context.Background()); err == nil {
		t.Fatal("Run accepted an invalid listen address")
	}
}

// TestServerRunDrainsOnContextCancel pins the graceful-shutdown contract:
// canceling the context stops accepting, drains and returns nil.
func TestServerRunDrainsOnContextCancel(t *testing.T) {
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}

	cfg := testServerConfig()
	server := NewServer(addr, &cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/", http.NoBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never accepted a connection: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}
}
