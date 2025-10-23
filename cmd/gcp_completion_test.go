package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigCompletionLocations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty zone",
			input:    "",
			expected: []string{""},
		},
		{
			name:     "zone value",
			input:    "us-central1-b",
			expected: []string{"us-central1-b", "us-central1"},
		},
		{
			name:     "region value",
			input:    "us-central1",
			expected: []string{"us-central1"},
		},
		{
			name:     "deduplicated values",
			input:    "europe-west2-b",
			expected: []string{"europe-west2-b", "europe-west2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := migCompletionLocations(tt.input)

			require.Len(t, result, len(tt.expected))
			for i, value := range tt.expected {
				require.Equal(t, value, result[i])
			}
		})
	}
}

func TestZoneLooksLikeZone(t *testing.T) {
	require.True(t, zoneLooksLikeZone("us-central1-a"))
	require.False(t, zoneLooksLikeZone("us-central1"))
}

func TestRegionFromZone(t *testing.T) {
	require.Equal(t, "us-central1", regionFromZone("us-central1-a"))
	require.Empty(t, regionFromZone("us-central1"))
}

func TestDeduplicateStrings(t *testing.T) {
	values := []string{"a", "b", "a", "c", "b"}
	result := deduplicateStrings(values)

	require.Len(t, result, 3)

	expected := []string{"a", "b", "c"}
	for i, value := range expected {
		require.Equal(t, value, result[i])
	}
}
