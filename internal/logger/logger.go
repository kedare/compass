// Package logger provides structured logging configuration for the compass application
package logger

import (
	"fmt"
	"os"
	"strings"
	"time"

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
	level           LogLevel
	logFile         *os.File
	consoleDisabled bool // When true, only file logging is active (for TUI mode)
}

// Disable temporarily disables console logging (for TUI mode).
// File logging continues if configured. Call Restore() to re-enable console output.
func (l *Logger) Disable() {
	l.consoleDisabled = true
	pterm.DisableOutput()
}

// Restore re-enables console logging after Disable() was called.
func (l *Logger) Restore() {
	l.consoleDisabled = false
	pterm.EnableOutput()
}

// Tracef logs a formatted message at trace level.
func (l *Logger) Tracef(format string, args ...interface{}) {
	if l.level <= LevelTrace {
		msg := fmt.Sprintf(format, args...)
		l.writeToFile("TRACE", msg)
		if !l.consoleDisabled {
			pterm.Debug.Printfln(format, args...)
		}
	}
}

// Debugf logs a formatted message at debug level.
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.level <= LevelDebug {
		msg := fmt.Sprintf(format, args...)
		l.writeToFile("DEBUG", msg)
		if !l.consoleDisabled {
			pterm.Debug.Printfln(format, args...)
		}
	}
}

// Infof logs a formatted message at info level.
func (l *Logger) Infof(format string, args ...interface{}) {
	if l.level <= LevelInfo {
		msg := fmt.Sprintf(format, args...)
		l.writeToFile("INFO", msg)
		if !l.consoleDisabled {
			pterm.Info.Printfln(format, args...)
		}
	}
}

// Warnf logs a formatted message at warn level.
func (l *Logger) Warnf(format string, args ...interface{}) {
	if l.level <= LevelWarn {
		msg := fmt.Sprintf(format, args...)
		l.writeToFile("WARN", msg)
		if !l.consoleDisabled {
			pterm.Warning.Printfln(format, args...)
		}
	}
}

// Errorf logs a formatted message at error level.
func (l *Logger) Errorf(format string, args ...interface{}) {
	if l.level <= LevelError {
		msg := fmt.Sprintf(format, args...)
		l.writeToFile("ERROR", msg)
		if !l.consoleDisabled {
			pterm.Error.Printfln(format, args...)
		}
	}
}

// Fatalf logs a formatted message at fatal level and exits the program.
func (l *Logger) Fatalf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.writeToFile("FATAL", msg)
	if !l.consoleDisabled {
		pterm.Error.Printfln(format, args...)
	}
	os.Exit(1)
}

// Trace logs a message at trace level.
func (l *Logger) Trace(args ...interface{}) {
	if l.level <= LevelTrace {
		msg := fmt.Sprint(args...)
		l.writeToFile("TRACE", msg)
		if !l.consoleDisabled {
			pterm.Debug.Println(args...)
		}
	}
}

// Debug logs a message at debug level.
func (l *Logger) Debug(args ...interface{}) {
	if l.level <= LevelDebug {
		msg := fmt.Sprint(args...)
		l.writeToFile("DEBUG", msg)
		if !l.consoleDisabled {
			pterm.Debug.Println(args...)
		}
	}
}

// Info logs a message at info level.
func (l *Logger) Info(args ...interface{}) {
	if l.level <= LevelInfo {
		msg := fmt.Sprint(args...)
		l.writeToFile("INFO", msg)
		if !l.consoleDisabled {
			pterm.Info.Println(args...)
		}
	}
}

// Warn logs a message at warn level.
func (l *Logger) Warn(args ...interface{}) {
	if l.level <= LevelWarn {
		msg := fmt.Sprint(args...)
		l.writeToFile("WARN", msg)
		if !l.consoleDisabled {
			pterm.Warning.Println(args...)
		}
	}
}

// Error logs a message at error level.
func (l *Logger) Error(args ...interface{}) {
	if l.level <= LevelError {
		msg := fmt.Sprint(args...)
		l.writeToFile("ERROR", msg)
		if !l.consoleDisabled {
			pterm.Error.Println(args...)
		}
	}
}

// Fatal logs a message at fatal level and exits the program.
func (l *Logger) Fatal(args ...interface{}) {
	msg := fmt.Sprint(args...)
	l.writeToFile("FATAL", msg)
	if !l.consoleDisabled {
		pterm.Error.Println(args...)
	}
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

// SetLogFile configures the logger to write to a file in addition to stdout.
// Pass an empty string to disable file logging.
func SetLogFile(path string) error {
	if Log.logFile != nil {
		_ = Log.logFile.Close()
		Log.logFile = nil
	}

	if path == "" {
		return nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	Log.logFile = file
	return nil
}

// CloseLogFile closes the log file if one is open.
func CloseLogFile() {
	if Log.logFile != nil {
		_ = Log.logFile.Close()
		Log.logFile = nil
	}
}

// writeToFile writes a log message to the file if configured.
func (l *Logger) writeToFile(level, message string) {
	if l.logFile != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		_, _ = fmt.Fprintf(l.logFile, "%s [%s] %s\n", timestamp, level, message)
	}
}
