package filterlist

// LookupMap is a helper type for matching a list of endpoint sources
// against a map.
type LookupMap map[string]struct{}

// Match returns Denied if a source in `list` is part of lm.
// If nothing is found, an empty string is returned.
func (lm LookupMap) Match(list []string) string {
	for _, l := range list {
		if _, ok := lm[l]; ok {
			return l
		}
	}

	return ""
}
