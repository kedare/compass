package tui

import "testing"

func TestParseFilterEmpty(t *testing.T) {
	expr := parseFilter("")
	if len(expr.terms) != 0 {
		t.Fatalf("expected 0 terms, got %d", len(expr.terms))
	}
}

func TestParseFilterWhitespace(t *testing.T) {
	expr := parseFilter("   ")
	if len(expr.terms) != 0 {
		t.Fatalf("expected 0 terms for whitespace, got %d", len(expr.terms))
	}
}

func TestParseFilterSingleTerm(t *testing.T) {
	expr := parseFilter("web")
	if len(expr.terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(expr.terms))
	}
	if expr.terms[0].negate {
		t.Fatal("expected non-negated term")
	}
	if len(expr.terms[0].alternatives) != 1 || expr.terms[0].alternatives[0] != "web" {
		t.Fatalf("expected [web], got %v", expr.terms[0].alternatives)
	}
}

func TestParseFilterNegation(t *testing.T) {
	expr := parseFilter("-dev")
	if len(expr.terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(expr.terms))
	}
	if !expr.terms[0].negate {
		t.Fatal("expected negated term")
	}
	if expr.terms[0].alternatives[0] != "dev" {
		t.Fatalf("expected [dev], got %v", expr.terms[0].alternatives)
	}
}

func TestParseFilterOR(t *testing.T) {
	expr := parseFilter("web|api")
	if len(expr.terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(expr.terms))
	}
	if len(expr.terms[0].alternatives) != 2 {
		t.Fatalf("expected 2 alternatives, got %d", len(expr.terms[0].alternatives))
	}
}

func TestParseFilterAND(t *testing.T) {
	expr := parseFilter("web prod")
	if len(expr.terms) != 2 {
		t.Fatalf("expected 2 terms, got %d", len(expr.terms))
	}
}

func TestParseFilterDashAlone(t *testing.T) {
	// A bare "-" should not produce a term (no content after dash)
	expr := parseFilter("-")
	if len(expr.terms) != 0 {
		t.Fatalf("expected 0 terms for bare dash, got %d", len(expr.terms))
	}
}

func TestParseFilterTrailingPipe(t *testing.T) {
	expr := parseFilter("web|")
	if len(expr.terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(expr.terms))
	}
	if len(expr.terms[0].alternatives) != 1 {
		t.Fatalf("expected 1 alternative (empty filtered out), got %d", len(expr.terms[0].alternatives))
	}
}

func TestFilterExprMatches(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		values   []string
		expected bool
	}{
		// Empty filter matches everything
		{"empty filter", "", []string{"anything"}, true},
		{"whitespace filter", "   ", []string{"anything"}, true},

		// Simple single term
		{"simple match", "web", []string{"web-server", "proj-a"}, true},
		{"simple no match", "api", []string{"web-server", "proj-a"}, false},
		{"case insensitive", "WEB", []string{"web-server", "proj-a"}, true},

		// AND (space separated)
		{"AND both match", "web prod", []string{"web-server", "prod-project", "us-central1"}, true},
		{"AND first fails", "api prod", []string{"web-server", "prod-project", "us-central1"}, false},
		{"AND second fails", "web staging", []string{"web-server", "prod-project", "us-central1"}, false},
		{"AND both fail", "api staging", []string{"web-server", "prod-project", "us-central1"}, false},
		{"AND same value", "web server", []string{"web-server"}, true},

		// OR (pipe separated)
		{"OR first match", "web|api", []string{"web-server"}, true},
		{"OR second match", "web|api", []string{"api-server"}, true},
		{"OR no match", "web|api", []string{"db-server"}, false},
		{"OR three alternatives", "web|api|db", []string{"db-server"}, true},

		// NOT (dash prefix)
		{"NOT excluded", "-dev", []string{"web-dev", "proj-a"}, false},
		{"NOT not excluded", "-dev", []string{"web-prod", "proj-a"}, true},
		{"NOT case insensitive", "-DEV", []string{"my-dev-server"}, false},

		// Combined: AND + OR
		{"AND+OR match", "compute web|api", []string{"compute.instance", "web-server"}, true},
		{"AND+OR second alt", "compute web|api", []string{"compute.instance", "api-gateway"}, true},
		{"AND+OR no match", "compute web|api", []string{"compute.instance", "db-server"}, false},

		// Combined: AND + NOT
		{"AND+NOT match", "compute -dev", []string{"compute.instance", "prod-vm", "us-central1"}, true},
		{"AND+NOT excluded", "compute -dev", []string{"compute.instance", "dev-vm", "us-central1"}, false},
		{"AND+NOT first fails", "storage -dev", []string{"compute.instance", "prod-vm"}, false},

		// Combined: NOT + OR (negated OR group)
		{"NOT+OR first excluded", "-dev|staging", []string{"web-dev", "proj-a"}, false},
		{"NOT+OR second excluded", "-dev|staging", []string{"web-staging", "proj-a"}, false},
		{"NOT+OR not excluded", "-dev|staging", []string{"web-prod", "proj-a"}, true},

		// Full combo: AND + OR + NOT
		{"full combo match", "compute prod|live -test", []string{"compute.instance", "prod-vm", "us-central1"}, true},
		{"full combo NOT fails", "compute prod|live -test", []string{"compute.instance", "prod-test-vm", "us-central1"}, false},
		{"full combo OR alt", "compute prod|live -test", []string{"compute.instance", "live-vm", "us-central1"}, true},

		// Edge: match across different values
		{"term in different columns", "compute us-central", []string{"compute.instance", "my-vm", "us-central1"}, true},

		// Edge: empty values
		{"empty values", "web", []string{"", "", ""}, false},
		{"some empty values", "web", []string{"", "web-server", ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := parseFilter(tt.filter)
			got := expr.matches(tt.values...)
			if got != tt.expected {
				t.Errorf("parseFilter(%q).matches(%v) = %v, want %v", tt.filter, tt.values, got, tt.expected)
			}
		})
	}
}
