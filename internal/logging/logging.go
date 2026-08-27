// Package logging configures the process-wide structured logger.
package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON structured logger writing to stdout, suitable for
// container log collection.
func New(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
