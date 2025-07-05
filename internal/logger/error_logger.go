package logger

import (
	"log"
)

// LogError logs an error message to stderr
func LogError(format string, args ...interface{}) {
	log.Printf(format, args...)
}

// LogErrorf is an alias for LogError for consistency
func LogErrorf(format string, args ...interface{}) {
	LogError(format, args...)
}
