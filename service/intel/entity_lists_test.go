package intel

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// oldMatchLists, oldListBlockReason, oldMakeDistinct and oldMakeMap are the
// previous implementations with maps, kept to check that the results are
// unchanged.

func oldMatchLists(e *Entity, lists []string) bool {
	if len(lists) == 0 {
		return false
	}

	e.BlockedByLists = nil
	e.BlockedEntities = nil

	lm := oldMakeMap(lists)
	for key, keyLists := range e.ListOccurences {
		for _, keyListID := range keyLists {
			if _, ok := lm[keyListID]; ok {
				e.BlockedByLists = append(e.BlockedByLists, keyListID)
				e.BlockedEntities = append(e.BlockedEntities, key)
			}
		}
	}

	e.BlockedByLists = oldMakeDistinct(e.BlockedByLists)
	e.BlockedEntities = oldMakeDistinct(e.BlockedEntities)

	return len(e.BlockedByLists) > 0
}

func oldListBlockReason(e *Entity) ListBlockReason {
	blockedBy := make([]ListMatch, len(e.BlockedEntities))

	lm := oldMakeMap(e.BlockedByLists)

	for idx, blockedEntity := range e.BlockedEntities {
		if entityLists, ok := e.ListOccurences[blockedEntity]; ok {
			var activeLists []string
			var inactiveLists []string

			for _, l := range entityLists {
				if _, ok := lm[l]; ok {
					activeLists = append(activeLists, l)
				} else {
					inactiveLists = append(inactiveLists, l)
				}
			}

			blockedBy[idx] = ListMatch{
				Entity:        blockedEntity,
				ActiveLists:   activeLists,
				InactiveLists: inactiveLists,
			}
		}
	}

	return blockedBy
}

func oldMakeDistinct(slice []string) []string {
	m := make(map[string]struct{}, len(slice))
	result := make([]string, 0, len(slice))

	for _, v := range slice {
		if _, ok := m[v]; ok {
			continue
		}

		m[v] = struct{}{}
		result = append(result, v)
	}

	return result
}

func oldMakeMap(slice []string) map[string]struct{} {
	lm := make(map[string]struct{})
	for _, v := range slice {
		lm[v] = struct{}{}
	}
	return lm
}

// randomStrings returns up to maxLen strings of a pool of poolSize strings
// with the given prefix, possibly with duplicates, or nil.
func randomStrings(rng *rand.Rand, prefix string, poolSize, maxLen int) []string {
	n := rng.IntN(maxLen + 1)
	if n == 0 && rng.IntN(2) == 0 {
		return nil
	}
	s := make([]string, n)
	for i := range s {
		s[i] = prefix + strconv.Itoa(rng.IntN(poolSize))
	}
	return s
}

func TestMakeDistinctMatchesMapImplementation(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Test input.
	for range 100000 {
		// Also cover inputs longer than the linear scan threshold.
		in := randomStrings(rng, "d", 1+rng.IntN(200), rng.IntN(150))
		want := oldMakeDistinct(slices.Clone(in))
		got := makeDistinct(slices.Clone(in))
		if !reflect.DeepEqual(got, want) || (got == nil) != (want == nil) {
			t.Fatalf("makeDistinct(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestMatchListsMatchesMapImplementation(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // Test input.
	for i := range 100000 {
		lists := randomStrings(rng, "L", 30, 40)

		// The order of the results of MatchLists depends on the iteration
		// order of ListOccurences, which is random: Compare the order only
		// for a single key.
		keys := 1
		if i%2 == 1 {
			keys = rng.IntN(12)
		}
		var occurrences map[string][]string
		if keys > 0 || rng.IntN(2) == 0 {
			occurrences = make(map[string][]string, keys)
		}
		for range keys {
			occurrences["k"+strconv.Itoa(rng.IntN(20))] = randomStrings(rng, "L", 40, 8)
		}

		want := &Entity{ListOccurences: occurrences}
		wantMatch := oldMatchLists(want, lists)
		got := &Entity{ListOccurences: occurrences}
		gotMatch := got.MatchLists(lists)

		if keys > 1 {
			// Sort a copy of the results of both.
			for _, e := range []*Entity{want, got} {
				if e.BlockedByLists != nil {
					e.BlockedByLists = slices.Sorted(slices.Values(e.BlockedByLists))
				}
				if e.BlockedEntities != nil {
					e.BlockedEntities = slices.Sorted(slices.Values(e.BlockedEntities))
				}
			}
		}
		if gotMatch != wantMatch ||
			!reflect.DeepEqual(got.BlockedByLists, want.BlockedByLists) ||
			!reflect.DeepEqual(got.BlockedEntities, want.BlockedEntities) {
			t.Fatalf("MatchLists(%q) of %q: got %v %q %q, want %v %q %q",
				lists, occurrences,
				gotMatch, got.BlockedByLists, got.BlockedEntities,
				wantMatch, want.BlockedByLists, want.BlockedEntities)
		}

		// The block reason only depends on the entity.
		if wantReason, gotReason := oldListBlockReason(got), got.ListBlockReason(); !reflect.DeepEqual(gotReason, wantReason) {
			t.Fatalf("ListBlockReason of %q, %q, %q: got %v, want %v",
				occurrences, got.BlockedByLists, got.BlockedEntities, gotReason, wantReason)
		}
	}
}

const (
	testDomain       = "www.example.com."
	testParentDomain = "example.com."
	testCNAME        = "e1.example.net."
)

// benchListIDs are the source IDs of the active filter lists. The default
// categories resolve to about this many sources.
var benchListIDs = []string{
	"TRAC-1", "TRAC-2", "TRAC-3", "TRAC-4", "TRAC-5",
	"MAL-1", "MAL-2", "MAL-3",
	"BAD-1", "BAD-2",
	"UNBREAK-1", "UNBREAK-2",
}

// BenchmarkMatchLists measures matching the list occurrences of an entity
// against the active filter lists, and the block reason if it matches.
func BenchmarkMatchLists(b *testing.B) {
	for _, bc := range []struct {
		name        string
		occurrences map[string][]string
	}{
		{name: "unlisted"},
		{name: "inactive", occurrences: map[string][]string{
			testParentDomain: {"SOC-1", "SOC-2"},
			"15133":          {"ASN-1"},
		}},
		{name: "listed", occurrences: map[string][]string{
			testParentDomain: {"MAL-2", "SOC-1", "TRAC-3"},
			testCNAME:        {"CDN-1", "TRAC-4"},
			"15133":          {"ASN-1"},
		}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			e := &Entity{ListOccurences: bc.occurrences}
			b.ReportAllocs()
			for b.Loop() {
				if e.MatchLists(benchListIDs) {
					_ = e.ListBlockReason()
				}
			}
		})
	}
}

// BenchmarkMakeDistinct measures removing duplicates from the domains to look
// up in the filter lists of a domain with a CNAME.
func BenchmarkMakeDistinct(b *testing.B) {
	domains := []string{
		testDomain, testParentDomain,
		testCNAME, "example.net.",
	}
	in := make([]string, len(domains))
	b.ReportAllocs()
	for b.Loop() {
		copy(in, domains)
		_ = makeDistinct(in)
	}
}
