// Package logger provides structured logging configuration for the compass application
package logger

import (
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
)

// Log provides a compatibility layer that mimics logrus API but uses pterm
var Log = &Logger{level: LevelInfo}

// LogLevel represents the severity level for log messages.
type LogLevel int

const (
	// LevelTrace is the most verbose log level.
	LevelTrace LogLevel = iota
	// LevelDebug is for debug-level messages.
	LevelDebug
	// LevelInfo is for informational messages.
	LevelInfo
	// LevelWarn is for warning messages.
	LevelWarn
	// LevelError is for error messages.
	LevelError
	// LevelFatal is for fatal errors that cause program termination.
	LevelFatal
	// LevelDisabled disables all logging.
	LevelDisabled
)

// Logger provides structured logging with configurable log levels.
type Logger struct {
	level         LogLevel
	previousLevel LogLevel
}

// Disable temporarily disables all logging. Call Restore() to re-enable.
func (l *Logger) Disable() {
	l.previousLevel = l.level
	l.level = LevelDisabled
	pterm.DisableOutput()
}

// Restore re-enables logging after Disable() was called.
func (l *Logger) Restore() {
	l.level = l.previousLevel
	pterm.EnableOutput()
}

// Tracef logs a formatted message at trace level.
func (l *Logger) Tracef(format string, args ...interface{}) {
	if l.level <= LevelTrace {
		pterm.Debug.Printfln(format, args...)
	}
}

// Debugf logs a formatted message at debug level.
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.level <= LevelDebug {
		pterm.Debug.Printfln(format, args...)
	}
}

// Infof logs a formatted message at info level.
func (l *Logger) Infof(format string, args ...interface{}) {
	if l.level <= LevelInfo {
		pterm.Info.Printfln(format, args...)
	}
}

// Warnf logs a formatted message at warn level.
func (l *Logger) Warnf(format string, args ...interface{}) {
	if l.level <= LevelWarn {
		pterm.Warning.Printfln(format, args...)
	}
}

// Errorf logs a formatted message at error level.
func (l *Logger) Errorf(format string, args ...interface{}) {
	if l.level <= LevelError {
		pterm.Error.Printfln(format, args...)
	}
}

// Fatalf logs a formatted message at fatal level and exits the program.
func (l *Logger) Fatalf(format string, args ...interface{}) {
	pterm.Error.Printfln(format, args...)
	os.Exit(1)
}

// Trace logs a message at trace level.
func (l *Logger) Trace(args ...interface{}) {
	if l.level <= LevelTrace {
		pterm.Debug.Println(args...)
	}
}

// Debug logs a message at debug level.
func (l *Logger) Debug(args ...interface{}) {
	if l.level <= LevelDebug {
		pterm.Debug.Println(args...)
	}
}

// Info logs a message at info level.
func (l *Logger) Info(args ...interface{}) {
	if l.level <= LevelInfo {
		pterm.Info.Println(args...)
	}
}

// Warn logs a message at warn level.
func (l *Logger) Warn(args ...interface{}) {
	if l.level <= LevelWarn {
		pterm.Warning.Println(args...)
	}
}

// Error logs a message at error level.
func (l *Logger) Error(args ...interface{}) {
	if l.level <= LevelError {
		pterm.Error.Println(args...)
	}
}

// Fatal logs a message at fatal level and exits the program.
func (l *Logger) Fatal(args ...interface{}) {
	pterm.Error.Println(args...)
	os.Exit(1)
}

// SetLevel configures the logger's minimum log level from a string.
func SetLevel(level string) error {
	switch strings.ToLower(level) {
	case "trace":
		Log.level = LevelTrace
		pterm.EnableDebugMessages()
	case "debug":
		Log.level = LevelDebug
		pterm.EnableDebugMessages()
	case "info":
		Log.level = LevelInfo
	case "warn", "warning":
		Log.level = LevelWarn
	case "error":
		Log.level = LevelError
	case "fatal":
		Log.level = LevelFatal
	default:
		return fmt.Errorf("invalid log level: %s", level)
	}

	return nil
}

// GetLogger returns the global logger instance.
func GetLogger() *Logger {
	return Log
}
