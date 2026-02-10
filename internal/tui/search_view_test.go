package tui

import "testing"

func TestHighlightMatch(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		term     string
		expected string
	}{
		{"empty term", "hello", "", "hello"},
		{"empty text", "", "hello", ""},
		{"exact match", "hello", "hello", "[yellow::b]hello[-:-:-]"},
		{"substring match", "my-web-server", "web", "my-[yellow::b]web[-:-:-]-server"},
		{"case insensitive", "MyWebServer", "web", "My[yellow::b]Web[-:-:-]Server"},
		{"no match", "hello", "xyz", "hello"},
		{"match at start", "web-server", "web", "[yellow::b]web[-:-:-]-server"},
		{"match at end", "my-web", "web", "my-[yellow::b]web[-:-:-]"},
		{"whitespace term", "hello", "  ", "hello"},
		{"term with spaces trimmed", "hello", " hell ", "[yellow::b]hell[-:-:-]o"},
	}

	for _, tt := range tests {
		got := highlightMatch(tt.text, tt.term)
		if got != tt.expected {
			t.Errorf("%s: highlightMatch(%q, %q) = %q, want %q", tt.name, tt.text, tt.term, got, tt.expected)
		}
	}
}
