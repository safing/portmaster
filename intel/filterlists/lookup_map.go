package filterlists

import "strings"

// LookupMap is a helper type for matching a list of endpoint sources
// against a map.
type LookupMap map[string]struct{}

// Match checks if a source in `list` is part of lm.
// Matches are joined to string and returned.
// If nothing is found, an empty string is returned.
func (lm LookupMap) Match(list []string) string {
	matches := make([]string, 0, len(list))
	for _, l := range list {
		if _, ok := lm[l]; ok {
			matches = append(matches, l)
		}
	}

	if len(matches) == 0 {
		return ""
	}

	return strings.Join(matches, ", ")
}
