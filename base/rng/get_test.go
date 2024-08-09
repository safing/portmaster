package rng

import (
	"testing"
)

func TestNumberRandomness(t *testing.T) {
	t.Parallel()

	// skip in automated tests
	t.Logf("Integer number bias test deactivated, as it sometimes triggers.")
	t.SkipNow()

	if testing.Short() {
		t.Skip()
	}

	var subjects uint64 = 10
	var testSize uint64 = 10000

	results := make([]uint64, int(subjects))
	for range int(subjects * testSize) {
		n, err := Number(subjects - 1)
		if err != nil {
			t.Fatal(err)
			return
		}
		results[int(n)]++
	}

	// catch big mistakes in the number function, eg. massive % bias
	lowerMargin := testSize - testSize/50
	upperMargin := testSize + testSize/50
	for subject, result := range results {
		if result < lowerMargin || result > upperMargin {
			t.Errorf("subject %d is outside of margins: %d", subject, result)
		}
	}

	t.Fatal(results)
}
