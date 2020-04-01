package intel

// ListSet holds a set of list IDs.
type ListSet struct {
	match []string
}

// NewListSet returns a new ListSet with the given list IDs.
func NewListSet(lists []string) *ListSet {
	// TODO: validate lists
	return &ListSet{
		match: lists,
	}
}

// Matches returns whether there is a match in the given list IDs.
func (ls *ListSet) Matches(lists []string) (matches bool) {
	for _, list := range lists {
		for _, entry := range ls.match {
			if entry == list {
				return true
			}
		}
	}

	return false
}

// MatchSet returns the matching list IDs.
func (ls *ListSet) MatchSet(lists []string) (matched []string) {
	for _, list := range lists {
		for _, entry := range ls.match {
			if entry == list {
				matched = append(matched, list)
			}
		}
	}

	return
}
