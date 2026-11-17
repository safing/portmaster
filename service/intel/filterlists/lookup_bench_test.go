package filterlists

import "testing"

// BenchmarkLookupDomainMiss measures looking up a domain that is in no filter
// list, which is the result of almost every lookup. Every new connection looks
// up its domain, its CNAMEs and all their parent domains.
func BenchmarkLookupDomainMiss(b *testing.B) {
	// Mark the (empty) filter lists as loaded.
	prevLoaded := filterListsLoaded
	filterListsLoaded = make(chan struct{})
	close(filterListsLoaded)
	b.Cleanup(func() { filterListsLoaded = prevLoaded })

	b.ReportAllocs()
	for b.Loop() {
		lists, err := LookupDomain("www.example.com.")
		if err != nil || len(lists) > 0 {
			b.Fatalf("unexpected result: %v, %v", lists, err)
		}
	}
}
