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
		// Fuzzy matching
		{"fuzzy chars in order", Query{Term: "prd", Fuzzy: true}, "production", true},
		{"fuzzy not in exact mode", Query{Term: "prd"}, "production", false},
		{"fuzzy case insensitive", Query{Term: "PRD", Fuzzy: true}, "production", true},
		{"fuzzy with separators", Query{Term: "abc", Fuzzy: true}, "a-big-cat", true},
		{"fuzzy substring still works", Query{Term: "prod", Fuzzy: true}, "my-production-vm", true},
		{"fuzzy no match", Query{Term: "xyz", Fuzzy: true}, "production", false},
		{"fuzzy empty term", Query{Term: "  ", Fuzzy: true}, "production", false},
	}

	for _, tt := range tests {
		if got := tt.query.Matches(tt.value); got != tt.result {
			t.Errorf("%s: expected %v got %v", tt.name, tt.result, got)
		}
	}
}

func TestQueryMatchesAny(t *testing.T) {
	tests := []struct {
		name   string
		query  Query
		values []string
		result bool
	}{
		{"empty term", Query{Term: "  "}, []string{"instance-a"}, false},
		{"match first value", Query{Term: "alpha"}, []string{"alpha-instance", "beta-instance"}, true},
		{"match second value", Query{Term: "beta"}, []string{"alpha-instance", "beta-instance"}, true},
		{"match on IP", Query{Term: "10.0.0"}, []string{"my-instance", "10.0.0.1", ""}, true},
		{"no match", Query{Term: "gamma"}, []string{"alpha", "beta"}, false},
		{"empty values", Query{Term: "foo"}, []string{}, false},
		{"skips empty strings", Query{Term: "foo"}, []string{"", "", ""}, false},
		{"case insensitive", Query{Term: "PROD"}, []string{"", "my-production-vm"}, true},
		{"single value match", Query{Term: "web"}, []string{"web-server-01"}, true},
		{"single value no match", Query{Term: "api"}, []string{"web-server-01"}, false},
		// Fuzzy matching
		{"fuzzy match on name", Query{Term: "prd", Fuzzy: true}, []string{"production-vm", "10.0.0.1"}, true},
		{"fuzzy match on second value", Query{Term: "abc", Fuzzy: true}, []string{"xyz", "a-big-cat"}, true},
		{"fuzzy no match any", Query{Term: "zzz", Fuzzy: true}, []string{"alpha", "beta"}, false},
		{"fuzzy not in exact mode", Query{Term: "prd"}, []string{"production-vm"}, false},
	}

	for _, tt := range tests {
		if got := tt.query.MatchesAny(tt.values...); got != tt.result {
			t.Errorf("%s: expected %v got %v", tt.name, tt.result, got)
		}
	}
}
