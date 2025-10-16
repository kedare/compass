package cmd

import (
	"testing"

	"cx/internal/logger"
)

func TestRootCommand(t *testing.T) {
	// Test that the root command is properly configured
	if rootCmd == nil {
		t.Error("rootCmd is nil")

		return
	}

	// Test command properties
	if rootCmd.Use != "cx" {
		t.Errorf("Expected Use to be 'cx', got %q", rootCmd.Use)
	}

	if rootCmd.Short == "" {
		t.Error("Short description is empty")
	}

	if rootCmd.Long == "" {
		t.Error("Long description is empty")
	}
}

func TestRootCommandFlags(t *testing.T) {
	// Test that persistent flags are properly configured
	logLevelFlag := rootCmd.PersistentFlags().Lookup("log-level")
	if logLevelFlag == nil {
		t.Error("log-level flag not found")

		return
	}

	if logLevelFlag.DefValue != "info" {
		t.Errorf("Expected log-level default value 'info', got %q", logLevelFlag.DefValue)
	}

	if logLevelFlag.Usage == "" {
		t.Error("log-level flag usage is empty")
	}
}

func TestRootCommandPersistentPreRun(t *testing.T) {
	// Test the PersistentPreRun function
	if rootCmd.PersistentPreRun == nil {
		t.Error("PersistentPreRun is not set")

		return
	}

	// Test with valid log level
	logLevel = "debug"

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PersistentPreRun panicked: %v", r)
		}
	}()

	rootCmd.PersistentPreRun(rootCmd, []string{})

	// Verify the log level was set
	if logger.Log.GetLevel().String() != "debug" {
		t.Errorf("Expected log level 'debug', got %q", logger.Log.GetLevel().String())
	}
}

func TestRootCommandInvalidLogLevel(t *testing.T) {
	// Test with invalid log level
	// We can't easily test the os.Exit(1) call, but we can test the validation
	err := logger.SetLevel("invalid-level")
	if err == nil {
		t.Error("Expected error for invalid log level")
	}
}

func TestExecute(t *testing.T) {
	// Test that Execute function exists and can be called
	// We can't test the full execution without proper setup

	// Reset args to avoid interference from previous tests
	rootCmd.SetArgs([]string{"--help"})

	err := Execute()
	if err != nil {
		t.Errorf("Execute() returned error: %v", err)
	}
}

func TestRootCommandSubcommands(t *testing.T) {
	// Test that subcommands are properly added
	commands := rootCmd.Commands()

	// Should have at least the gcp command plus built-in commands (help, completion)
	if len(commands) < 2 {
		t.Errorf("Expected at least 2 commands, got %d", len(commands))
	}

	// Check for gcp command specifically
	hasGcp := false

	for _, cmd := range commands {
		if cmd.Name() == "gcp" {
			hasGcp = true

			break
		}
	}

	if !hasGcp {
		t.Error("gcp command not found in root command")
	}
}

func TestLogLevelValues(t *testing.T) {
	// Test various log level values
	validLevels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}

	for _, level := range validLevels {
		t.Run("level_"+level, func(t *testing.T) {
			err := logger.SetLevel(level)
			if err != nil {
				t.Errorf("Valid log level %q returned error: %v", level, err)
			}
		})
	}

	// Test invalid levels
	invalidLevels := []string{"invalid", "verbose", "critical", ""}

	for _, level := range invalidLevels {
		t.Run("invalid_level_"+level, func(t *testing.T) {
			err := logger.SetLevel(level)
			if err == nil {
				t.Errorf("Invalid log level %q should return error", level)
			}
		})
	}
}
