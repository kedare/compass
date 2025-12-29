package search

import "testing"

func TestQueryMatches(t *testing.T) {
	tests := []struct {
		name   string
		query  Query
		value  string
		result bool
	}{
		{"empty term", Query{Term: "  "}, "instance-a", false},
		{"case insensitive", Query{Term: "PiOu"}, "my-piou-instance", true},
		{"no match", Query{Term: "foo"}, "bar", false},
	}

	for _, tt := range tests {
		if got := tt.query.Matches(tt.value); got != tt.result {
			t.Errorf("%s: expected %v got %v", tt.name, tt.result, got)
		}
	}
}
