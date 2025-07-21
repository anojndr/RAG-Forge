package logger

import (
	"fmt"
	"log/slog"
)

// LogError logs an error message to stderr
func LogError(format string, args ...interface{}) {
	slog.Error(fmt.Sprintf(format, args...))
}
