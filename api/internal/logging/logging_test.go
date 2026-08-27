package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestNew_LevelParsing(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		level          string
		wantDebug      bool
		wantInfo       bool
		wantWarnAlways bool
	}{
		{name: "debug enables debug and above", level: "debug", wantDebug: true, wantInfo: true, wantWarnAlways: true},
		{name: "info enables info and above but not debug", level: "info", wantDebug: false, wantInfo: true, wantWarnAlways: true},
		{name: "warn enables warn and above but not info", level: "warn", wantDebug: false, wantInfo: false, wantWarnAlways: true},
		{name: "invalid level falls back to info", level: "not-a-level", wantDebug: false, wantInfo: true, wantWarnAlways: true},
		{name: "empty level falls back to info", level: "", wantDebug: false, wantInfo: true, wantWarnAlways: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.level)
			if logger == nil {
				t.Fatal("New() returned nil logger")
			}
			if got := logger.Enabled(ctx, slog.LevelDebug); got != tt.wantDebug {
				t.Errorf("Enabled(Debug) = %v, want %v", got, tt.wantDebug)
			}
			if got := logger.Enabled(ctx, slog.LevelInfo); got != tt.wantInfo {
				t.Errorf("Enabled(Info) = %v, want %v", got, tt.wantInfo)
			}
			if got := logger.Enabled(ctx, slog.LevelWarn); got != tt.wantWarnAlways {
				t.Errorf("Enabled(Warn) = %v, want %v", got, tt.wantWarnAlways)
			}
		})
	}
}
