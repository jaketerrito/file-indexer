package logger

import (
	"log/slog"
	"testing"
)

func TestSetupSetsDefaultLoggerAtLevel(t *testing.T) {
	Setup(slog.LevelWarn)

	l := slog.Default()
	ctx := t.Context()

	if l.Enabled(ctx, slog.LevelWarn) != true {
		t.Errorf("Warn level should be enabled after Setup(LevelWarn)")
	}
	if l.Enabled(ctx, slog.LevelError) != true {
		t.Errorf("Error level should be enabled after Setup(LevelWarn)")
	}
	if l.Enabled(ctx, slog.LevelInfo) != false {
		t.Errorf("Info level should be disabled after Setup(LevelWarn)")
	}
}

func TestSetupRespectsDebugLevel(t *testing.T) {
	Setup(slog.LevelDebug)

	if !slog.Default().Enabled(t.Context(), slog.LevelDebug) {
		t.Errorf("Debug level should be enabled after Setup(LevelDebug)")
	}
}
