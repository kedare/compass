package logger

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/pterm/pterm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureOutput(f func()) string {
	old := pterm.Info.Writer
	r, w, _ := os.Pipe()
	pterm.Info.Writer = w
	pterm.Success.Writer = w
	pterm.Warning.Writer = w
	pterm.Error.Writer = w
	pterm.Debug.Writer = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()

	f()
	_ = w.Close()
	out := <-outC

	pterm.Info.Writer = old
	pterm.Success.Writer = old
	pterm.Warning.Writer = old
	pterm.Error.Writer = old
	pterm.Debug.Writer = old

	return out
}

func TestSetLevel(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		expectLevel LogLevel
		expectError bool
	}{
		{"trace", "trace", LevelTrace, false},
		{"debug", "debug", LevelDebug, false},
		{"info", "info", LevelInfo, false},
		{"warn", "warn", LevelWarn, false},
		{"warning", "warning", LevelWarn, false},
		{"error", "error", LevelError, false},
		{"fatal", "fatal", LevelFatal, false},
		{"uppercase", "INFO", LevelInfo, false},
		{"mixed case", "WaRn", LevelWarn, false},
		{"invalid", "invalid", LevelInfo, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetLevel(tt.level)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid log level")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectLevel, Log.level)
			}
		})
	}
}

func TestGetLogger(t *testing.T) {
	logger := GetLogger()
	require.NotNil(t, logger)
	assert.Equal(t, Log, logger)
}

func TestLoggerLevels(t *testing.T) {
	t.Run("trace_level_logs_everything", func(t *testing.T) {
		Log.level = LevelTrace
		pterm.EnableDebugMessages()

		output := captureOutput(func() {
			Log.Trace("trace message")
			Log.Tracef("trace %s", "formatted")
		})
		assert.Contains(t, output, "trace message")
		assert.Contains(t, output, "trace formatted")
	})

	t.Run("debug_level_logs_debug_and_above", func(t *testing.T) {
		Log.level = LevelDebug
		pterm.EnableDebugMessages()

		output := captureOutput(func() {
			Log.Debug("debug message")
			Log.Debugf("debug %s", "formatted")
		})
		assert.Contains(t, output, "debug message")
		assert.Contains(t, output, "debug formatted")
	})

	t.Run("info_level_logs_info_and_above", func(t *testing.T) {
		Log.level = LevelInfo

		output := captureOutput(func() {
			Log.Info("info message")
			Log.Infof("info %s", "formatted")
		})
		assert.Contains(t, output, "info message")
		assert.Contains(t, output, "info formatted")
	})

	t.Run("warn_level_logs_warn_and_above", func(t *testing.T) {
		Log.level = LevelWarn

		output := captureOutput(func() {
			Log.Warn("warn message")
			Log.Warnf("warn %s", "formatted")
		})
		assert.Contains(t, output, "warn message")
		assert.Contains(t, output, "warn formatted")
	})

	t.Run("error_level_logs_errors", func(t *testing.T) {
		Log.level = LevelError

		output := captureOutput(func() {
			Log.Error("error message")
			Log.Errorf("error %s", "formatted")
		})
		assert.Contains(t, output, "error message")
		assert.Contains(t, output, "error formatted")
	})

	t.Run("higher_level_blocks_lower_messages", func(t *testing.T) {
		Log.level = LevelError

		output := captureOutput(func() {
			Log.Info("should not appear")
			Log.Warn("should not appear")
			Log.Error("should appear")
		})
		assert.NotContains(t, output, "should not appear")
		assert.Contains(t, output, "should appear")
	})
}

func TestInitPterm(t *testing.T) {
	// Save original writers
	origInfoWriter := pterm.Info.Writer
	origSuccessWriter := pterm.Success.Writer
	origWarningWriter := pterm.Warning.Writer
	origErrorWriter := pterm.Error.Writer
	origDebugWriter := pterm.Debug.Writer

	// Call InitPterm
	InitPterm()

	// Verify all writers are set to stderr
	assert.Equal(t, os.Stderr, pterm.Info.Writer)
	assert.Equal(t, os.Stderr, pterm.Success.Writer)
	assert.Equal(t, os.Stderr, pterm.Warning.Writer)
	assert.Equal(t, os.Stderr, pterm.Error.Writer)
	assert.Equal(t, os.Stderr, pterm.Debug.Writer)

	// Restore original writers
	pterm.Info.Writer = origInfoWriter
	pterm.Success.Writer = origSuccessWriter
	pterm.Warning.Writer = origWarningWriter
	pterm.Error.Writer = origErrorWriter
	pterm.Debug.Writer = origDebugWriter
}

func TestLogLevelConstants(t *testing.T) {
	// Verify log levels are in correct order
	assert.Less(t, int(LevelTrace), int(LevelDebug))
	assert.Less(t, int(LevelDebug), int(LevelInfo))
	assert.Less(t, int(LevelInfo), int(LevelWarn))
	assert.Less(t, int(LevelWarn), int(LevelError))
	assert.Less(t, int(LevelError), int(LevelFatal))
}
