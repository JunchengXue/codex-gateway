package cli

import (
	"log/slog"
	"os"

	"github.com/Collections/Agents/codex-gateway/internal/logging"
)

func newRootLogger(level string) *slog.Logger {
	return logging.New(level, os.Stdout)
}
