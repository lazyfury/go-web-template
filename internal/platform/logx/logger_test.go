package logx

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		level     string
		format    string
		wantErr   bool
		wantLevel zapcore.Level
	}{
		{name: "debug json", level: "debug", format: "json", wantLevel: zapcore.DebugLevel},
		{name: "warn console", level: "warn", format: "console", wantLevel: zapcore.WarnLevel},
		{name: "empty falls back to info/json", level: "", format: "", wantLevel: zapcore.InfoLevel},
		{name: "invalid level", level: "verbose", format: "json", wantErr: true},
		{name: "invalid format", level: "info", format: "xml", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, err := New(tt.level, tt.format)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if logger != nil {
					t.Fatal("expected nil logger on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = logger.Sync() })
			if !logger.Core().Enabled(tt.wantLevel) {
				t.Errorf("level %s not enabled", tt.wantLevel)
			}
			if tt.wantLevel > zapcore.DebugLevel && logger.Core().Enabled(zapcore.DebugLevel) {
				t.Errorf("debug should be disabled at level %s", tt.wantLevel)
			}
		})
	}
}
