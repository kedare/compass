package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdateCommandFlags ensures the update command exposes the expected flags.
func TestUpdateCommandFlags(t *testing.T) {
	t.Helper()

	require.NotNil(t, updateCmd.Flags().Lookup("check"))
	require.NotNil(t, updateCmd.Flags().Lookup("force"))
}
