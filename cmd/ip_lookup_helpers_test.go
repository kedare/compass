package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/stretchr/testify/require"
)

func TestDedupeAssociations(t *testing.T) {
	tests := []struct {
		name     string
		input    []gcp.IPAssociation
		expected int
	}{
		{
			name:     "empty input",
			input:    []gcp.IPAssociation{},
			expected: 0,
		},
		{
			name: "no duplicates",
			input: []gcp.IPAssociation{
				{Project: "p1", Kind: "instance", Resource: "r1", Location: "us-central1-a", IPAddress: "1.2.3.4", Details: "d1"},
				{Project: "p2", Kind: "instance", Resource: "r2", Location: "us-central1-b", IPAddress: "1.2.3.5", Details: "d2"},
			},
			expected: 2,
		},
		{
			name: "exact duplicates",
			input: []gcp.IPAssociation{
				{Project: "p1", Kind: "instance", Resource: "r1", Location: "us-central1-a", IPAddress: "1.2.3.4", Details: "d1"},
				{Project: "p1", Kind: "instance", Resource: "r1", Location: "us-central1-a", IPAddress: "1.2.3.4", Details: "d1"},
			},
			expected: 1,
		},
		{
			name: "mixed duplicates and unique",
			input: []gcp.IPAssociation{
				{Project: "p1", Kind: "instance", Resource: "r1", Location: "us-central1-a", IPAddress: "1.2.3.4", Details: "d1"},
				{Project: "p1", Kind: "instance", Resource: "r1", Location: "us-central1-a", IPAddress: "1.2.3.4", Details: "d1"},
				{Project: "p2", Kind: "instance", Resource: "r2", Location: "us-central1-b", IPAddress: "1.2.3.5", Details: "d2"},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dedupeAssociations(tt.input)
			require.Len(t, result, tt.expected)
		})
	}
}

func TestIsContextError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: true,
		},
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "wrapped context canceled",
			err:      errors.Join(context.Canceled, errors.New("something else")),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isContextError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}
