package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func New(level string, out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: parseLevel(level),
	}))
}

func parseLevel(in string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
