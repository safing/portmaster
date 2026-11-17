package bbolt

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/safing/portmaster/base/database/query"
)

// TestPurgeReportsCancellation checks that a cancelled purge tells the caller
// that it did not complete, instead of reporting success.
func TestPurgeReportsCancellation(t *testing.T) {
	t.Parallel()

	b := newPutManyTestDB(t)
	for i := range 10 {
		_, err := b.Put(newPutManyTestRecord(fmt.Sprintf("purge/%d", i)))
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, shadowDelete := range []bool{true, false} {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		n, err := b.Purge(ctx, query.New("test:purge/").MustBeValid(), true, true, shadowDelete)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("shadowDelete=%v: expected context.Canceled, got %d purged and error %v", shadowDelete, n, err)
		}
		if n != 0 {
			t.Errorf("shadowDelete=%v: %d records purged with a cancelled context", shadowDelete, n)
		}
	}

	// Nothing was purged.
	for i := range 10 {
		_, err := b.Get(fmt.Sprintf("purge/%d", i))
		if err != nil {
			t.Fatalf("record purge/%d: %s", i, err)
		}
	}
}
