// Package logging provides structured logging via zerolog.
package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// NewLogger creates a configured zerolog.Logger.
//
// Format selection:
//   - "json"    → newline-delimited JSON (default, required for CloudWatch / ECS)
//   - "console" → human-readable coloured output for local dev
//   - "auto"    → JSON when stdout is not a TTY (CI, containers), console otherwise
//
// In production / ECS always set FACT0_LOG_FORMAT=json (or leave it at
// the default). The console format writes ANSI escape codes that appear as
// garbage in CloudWatch Logs.
func NewLogger(level string, format string) zerolog.Logger {
	w := chooseWriter(format)

	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	// Caller() adds file+line which is useful locally. In JSON mode zerolog
	// trims it to a short relative path; in console mode it prints in full.
	// Keep it on for both - it costs almost nothing and is invaluable when
	// reading CloudWatch stack traces.
	return zerolog.New(w).
		Level(lvl).
		With().
		Timestamp().
		Caller().
		Str("service", "fact0").
		Logger()
}

func chooseWriter(format string) io.Writer {
	switch format {
	case "console":
		return zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	case "json":
		return os.Stdout
	default:
		// "auto": use console only when stdout is a real TTY. This means
		// `go run ./fact0` in a terminal gets pretty output, but the
		// same binary in Docker / ECS / CI automatically emits JSON.
		if isTTY() {
			return zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: time.RFC3339,
			}
		}
		return os.Stdout
	}
}

// isTTY returns true when os.Stdout is an interactive terminal.
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
