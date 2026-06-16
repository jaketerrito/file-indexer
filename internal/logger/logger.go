package logger

import (
	"log/slog"
	"os"
)

// Setup configures the default slog logger to emit JSON logs at the given
// level. All slog package-level functions (slog.Info, slog.Error, etc.) use
// this configuration after Setup is called.
func Setup(level slog.Level) {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}
