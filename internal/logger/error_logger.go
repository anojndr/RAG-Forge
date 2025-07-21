package logger

import (
	"log/slog"
)

// LogError logs an error message to stderr
func LogError(format string, args ...interface{}) {
	slog.Error(format, args...)
}
