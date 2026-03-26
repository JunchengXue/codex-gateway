package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

const LevelTrace = slog.Level(-8)

func New(level string, out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}
	lvl := ParseLevel(level)
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				if a.Value.Any().(slog.Level) == LevelTrace {
					a.Value = slog.StringValue("TRACE")
				}
			}
			return a
		},
	}))
}

func ParseLevel(in string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "trace":
		return LevelTrace
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}
