package intel

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func mergeStringListMap(a, b []string) []string {
	listMap := make(map[string]struct{})
	for _, s := range a {
		listMap[s] = struct{}{}
	}
	for _, s := range b {
		listMap[s] = struct{}{}
	}

	res := make([]string, 0, len(listMap))
	for s := range listMap {
		res = append(res, s)
	}
	sort.Strings(res)
	return res
}

func randStrings(r *rand.Rand, n int) []string {
	if n < 0 {
		return nil
	}
	s := make([]string, n)
	for i := range s {
		s[i] = fmt.Sprintf("list%d", r.Intn(12))
	}
	return s
}

func TestMergeStringListMatchesMap(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1)) //nolint:gosec
	for range 50000 {
		a, b := randStrings(r, r.Intn(12)-1), randStrings(r, r.Intn(12)-1)
		want := mergeStringListMap(a, b)
		got := mergeStringList(a, b)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("mergeStringList(%v, %v) = %#v, want %#v", a, b, got, want)
		}
	}
}

func BenchmarkMergeStringList(b *testing.B) {
	for _, n := range []int{0, 1, 3, 10} {
		r := rand.New(rand.NewSource(2)) //nolint:gosec
		x, y := randStrings(r, n), randStrings(r, n)
		b.Run(fmt.Sprintf("n%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = mergeStringList(nil, y)
				_ = mergeStringList(x, y)
			}
		})
	}
}
