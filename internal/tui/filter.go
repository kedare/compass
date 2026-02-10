package tui

import "strings"

// filterTerm is a single token: a literal (possibly OR-grouped) with optional negation.
type filterTerm struct {
	alternatives []string // OR-separated values, e.g. "prod|staging" → ["prod", "staging"]
	negate       bool     // true if prefixed with "-"
}

// filterExpr is a parsed filter: all terms must match (AND logic).
type filterExpr struct {
	terms []filterTerm
}

// parseFilter splits a raw filter string into an expression.
// Spaces separate AND terms. "|" separates OR alternatives within a term.
// "-" prefix negates a term.
//
// Examples:
//
//	"web prod"       → must contain "web" AND "prod"
//	"web|api"        → must contain "web" OR "api"
//	"-dev"           → must NOT contain "dev"
//	"compute -dev|staging" → must contain "compute" AND must NOT contain "dev" or "staging"
func parseFilter(raw string) filterExpr {
	var terms []filterTerm

	for _, token := range strings.Fields(raw) {
		if token == "-" {
			continue
		}

		negate := false
		if strings.HasPrefix(token, "-") && len(token) > 1 {
			negate = true
			token = token[1:]
		}

		alts := strings.Split(strings.ToLower(token), "|")

		// Remove empty alternatives
		var cleaned []string
		for _, a := range alts {
			if a != "" {
				cleaned = append(cleaned, a)
			}
		}

		if len(cleaned) > 0 {
			terms = append(terms, filterTerm{alternatives: cleaned, negate: negate})
		}
	}

	return filterExpr{terms: terms}
}

// matches reports whether any of the provided values satisfy the entire filter expression.
// All terms must be satisfied (AND). Within a term, any alternative suffices (OR).
func (f filterExpr) matches(values ...string) bool {
	if len(f.terms) == 0 {
		return true
	}

	// Pre-lowercase all values once
	lower := make([]string, len(values))
	for i, v := range values {
		lower[i] = strings.ToLower(v)
	}

	for _, term := range f.terms {
		found := false

		for _, alt := range term.alternatives {
			for _, v := range lower {
				if strings.Contains(v, alt) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}

		if term.negate && found {
			return false
		}

		if !term.negate && !found {
			return false
		}
	}

	return true
}
