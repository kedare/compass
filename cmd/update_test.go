package cmd

import "testing"

// TestUpdateCommandFlags ensures the update command exposes the expected flags.
func TestUpdateCommandFlags(t *testing.T) {
	t.Helper()

	if flag := updateCmd.Flags().Lookup("check"); flag == nil {
		t.Fatalf("expected --check flag to be registered")
	}

	if flag := updateCmd.Flags().Lookup("force"); flag == nil {
		t.Fatalf("expected --force flag to be registered")
	}
}
