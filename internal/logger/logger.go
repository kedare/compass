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

type LogLevel int

const (
	LevelTrace LogLevel = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

type Logger struct {
	level LogLevel
}

func (l *Logger) Tracef(format string, args ...interface{}) {
	if l.level <= LevelTrace {
		pterm.Debug.Printfln(format, args...)
	}
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.level <= LevelDebug {
		pterm.Debug.Printfln(format, args...)
	}
}

func (l *Logger) Infof(format string, args ...interface{}) {
	if l.level <= LevelInfo {
		pterm.Info.Printfln(format, args...)
	}
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	if l.level <= LevelWarn {
		pterm.Warning.Printfln(format, args...)
	}
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	if l.level <= LevelError {
		pterm.Error.Printfln(format, args...)
	}
}

func (l *Logger) Fatalf(format string, args ...interface{}) {
	pterm.Error.Printfln(format, args...)
	os.Exit(1)
}

func (l *Logger) Trace(args ...interface{}) {
	if l.level <= LevelTrace {
		pterm.Debug.Println(args...)
	}
}

func (l *Logger) Debug(args ...interface{}) {
	if l.level <= LevelDebug {
		pterm.Debug.Println(args...)
	}
}

func (l *Logger) Info(args ...interface{}) {
	if l.level <= LevelInfo {
		pterm.Info.Println(args...)
	}
}

func (l *Logger) Warn(args ...interface{}) {
	if l.level <= LevelWarn {
		pterm.Warning.Println(args...)
	}
}

func (l *Logger) Error(args ...interface{}) {
	if l.level <= LevelError {
		pterm.Error.Println(args...)
	}
}

func (l *Logger) Fatal(args ...interface{}) {
	pterm.Error.Println(args...)
	os.Exit(1)
}

func SetLevel(level string) error {
	switch strings.ToLower(level) {
	case "trace":
		Log.level = LevelTrace
	case "debug":
		Log.level = LevelDebug
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

func GetLogger() *Logger {
	return Log
}
