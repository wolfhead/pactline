// Package logging configures the process-wide structured logger.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup returns a JSON logger at the given level and installs it as the default.
func Setup(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
	slog.SetDefault(l)
	return l
}
