package cmd

import "testing"

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

			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d locations, got %d", len(tt.expected), len(result))
			}

			for i, value := range tt.expected {
				if result[i] != value {
					t.Fatalf("expected %q at index %d, got %q", value, i, result[i])
				}
			}
		})
	}
}

func TestZoneLooksLikeZone(t *testing.T) {
	if !zoneLooksLikeZone("us-central1-a") {
		t.Fatal("expected zone value to be detected")
	}

	if zoneLooksLikeZone("us-central1") {
		t.Fatal("expected region value to be rejected")
	}
}

func TestRegionFromZone(t *testing.T) {
	if region := regionFromZone("us-central1-a"); region != "us-central1" {
		t.Fatalf("expected region 'us-central1', got %q", region)
	}

	if region := regionFromZone("us-central1"); region != "" {
		t.Fatalf("expected empty region, got %q", region)
	}
}

func TestDeduplicateStrings(t *testing.T) {
	values := []string{"a", "b", "a", "c", "b"}
	result := deduplicateStrings(values)

	if len(result) != 3 {
		t.Fatalf("expected 3 unique values, got %d", len(result))
	}

	expected := []string{"a", "b", "c"}
	for i, value := range expected {
		if result[i] != value {
			t.Fatalf("expected %q at index %d, got %q", value, i, result[i])
		}
	}
}
