package log

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Logger provides structured logging with JSON output
type Logger struct {
	level string
}

// New creates a new Logger instance
func New(level string) *Logger {
	return &Logger{
		level: level,
	}
}

// log writes a structured log entry
func (l *Logger) log(level string, msg string, fields map[string]interface{}) {
	entry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"level":     level,
		"msg":       msg,
	}

	// Merge fields into entry
	for k, v := range fields {
		entry[k] = v
	}

	data, _ := json.Marshal(entry)
	fmt.Fprintln(os.Stdout, string(data))
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	l.log("info", msg, fields)
}

// Error logs an error message
func (l *Logger) Error(msg string, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	l.log("error", msg, fields)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	l.log("warn", msg, fields)
}

// Debug logs a debug message (checks level)
func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	if l.level != "debug" {
		return
	}
	if fields == nil {
		fields = make(map[string]interface{})
	}
	l.log("debug", msg, fields)
}
