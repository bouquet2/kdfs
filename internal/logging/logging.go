package logging

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const envLogFormat = "KDFS_LOG_FORMAT"

func normalizeFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return "json"
	default:
		return "pretty"
	}
}

func newLogger(w io.Writer, component, format string) zerolog.Logger {
	writer := w
	if normalizeFormat(format) == "pretty" {
		writer = zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339}
	}
	logger := zerolog.New(writer).With().Timestamp().Logger()
	if component != "" {
		logger = logger.With().Str("component", component).Logger()
	}
	return logger
}

func Root() zerolog.Logger {
	return newLogger(os.Stderr, "", os.Getenv(envLogFormat))
}

func Component(name string) zerolog.Logger {
	return newLogger(os.Stderr, name, os.Getenv(envLogFormat))
}
