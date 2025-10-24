package cmd

import (
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/stretchr/testify/assert"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:  "single label",
			input: "env=prod",
			expected: map[string]string{
				"env": "prod",
			},
		},
		{
			name:  "multiple labels",
			input: "env=prod,team=infrastructure,region=us-central1",
			expected: map[string]string{
				"env":    "prod",
				"team":   "infrastructure",
				"region": "us-central1",
			},
		},
		{
			name:  "labels with whitespace",
			input: " env = prod , team = infrastructure ",
			expected: map[string]string{
				"env":  "prod",
				"team": "infrastructure",
			},
		},
		{
			name:  "label with equals in value",
			input: "formula=a=b+c",
			expected: map[string]string{
				"formula": "a=b+c",
			},
		},
		{
			name:     "invalid label without equals",
			input:    "invalid",
			expected: map[string]string{},
		},
		{
			name:  "mixed valid and invalid",
			input: "env=prod,invalid,team=infra",
			expected: map[string]string{
				"env":  "prod",
				"team": "infra",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLabels(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsTestComplete(t *testing.T) {
	tests := []struct {
		name     string
		result   *gcp.ConnectivityTestResult
		expected bool
	}{
		{
			name: "nil reachability details",
			result: &gcp.ConnectivityTestResult{
				ReachabilityDetails: nil,
			},
			expected: false,
		},
		{
			name: "empty result",
			result: &gcp.ConnectivityTestResult{
				ReachabilityDetails: &gcp.ReachabilityDetails{
					Result: "",
				},
			},
			expected: false,
		},
		{
			name: "result present",
			result: &gcp.ConnectivityTestResult{
				ReachabilityDetails: &gcp.ReachabilityDetails{
					Result: "REACHABLE",
				},
			},
			expected: true,
		},
		{
			name: "unreachable result",
			result: &gcp.ConnectivityTestResult{
				ReachabilityDetails: &gcp.ReachabilityDetails{
					Result: "UNREACHABLE",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTestComplete(tt.result)
			assert.Equal(t, tt.expected, result)
		})
	}
}
