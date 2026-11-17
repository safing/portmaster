package bbolt

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
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

// Record kinds used by TestPurgeAcrossChunks.
const (
	purgeKindLive = iota
	purgeKindSecret
	purgeKindDeleted
	numPurgeKinds
)

// TestPurgeAcrossChunks checks that a purge spanning several chunks purges
// every matching record exactly once and leaves all other records untouched,
// also when the chunks end next to records that are skipped.
func TestPurgeAcrossChunks(t *testing.T) {
	t.Parallel()

	for _, shadowDelete := range []bool{true, false} {
		t.Run(fmt.Sprintf("shadowDelete=%v", shadowDelete), func(t *testing.T) {
			t.Parallel()

			b := newPutManyTestDB(t)
			now := time.Now().Unix()
			deletedAt := now - 3600

			// More than three chunks (3334 records) of live records, interleaved with
			// records that must be skipped, and records around the prefix.
			const total = 10000
			metas := make(map[string]*record.Meta, total+4)
			wantPurged := 0
			for i := range total {
				m := &record.Meta{Created: now, Modified: now}
				switch i % numPurgeKinds {
				case purgeKindLive:
					wantPurged++
				case purgeKindSecret:
					m.MakeSecret()
				case purgeKindDeleted:
					m.Deleted = deletedAt
				}
				metas[fmt.Sprintf("purge/%05d", i)] = m
			}
			for _, key := range []string{"purga/0", "purge", "purgf/0", "zzz"} {
				metas[key] = &record.Meta{Created: now, Modified: now}
			}
			putRawRecords(t, b, metas)

			n, err := b.Purge(t.Context(), query.New("test:purge/").MustBeValid(), true, false, shadowDelete)
			if err != nil {
				t.Fatal(err)
			}
			if n != wantPurged {
				t.Errorf("purged %d records, expected %d", n, wantPurged)
			}

			after := readAllMetas(t, b)
			for key, before := range metas {
				m, exists := after[key]
				var i int
				_, scanErr := fmt.Sscanf(key, "purge/%05d", &i)
				switch {
				case scanErr != nil || i%numPurgeKinds != purgeKindLive:
					// Not to be purged: must be unchanged.
					if !exists {
						t.Fatalf("record %s was removed", key)
					}
					if m.Deleted != before.Deleted {
						t.Fatalf("record %s: deleted changed from %d to %d", key, before.Deleted, m.Deleted)
					}
				case shadowDelete:
					if !exists || !m.IsDeleted() {
						t.Fatalf("record %s was not shadow deleted", key)
					}
				default:
					if exists {
						t.Fatalf("record %s was not deleted", key)
					}
				}
			}

			// A second purge finds nothing left to purge.
			n, err = b.Purge(t.Context(), query.New("test:purge/").MustBeValid(), true, false, shadowDelete)
			if err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("second purge purged %d records", n)
			}
		})
	}
}

// BenchmarkPurge measures purging n matching records. With shadowDelete, the
// purged records stay in the bucket, so a purge that rescans them from the
// prefix for every chunk does quadratic work.
func BenchmarkPurge(b *testing.B) {
	for _, shadowDelete := range []bool{true, false} {
		for _, n := range []int{2000, 8000, 32000} {
			b.Run(fmt.Sprintf("shadowDelete=%v/n=%d", shadowDelete, n), func(b *testing.B) {
				benchmarkPurge(b, n, shadowDelete)
			})
		}
	}
}

func benchmarkPurge(b *testing.B, n int, shadowDelete bool) {
	b.Helper()

	db, err := NewBBolt("test", b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	bb, ok := db.(*BBolt)
	if !ok {
		b.Fatal("unexpected storage type")
	}
	defer func() {
		_ = bb.Shutdown()
	}()

	now := time.Now().Unix()
	metas := make(map[string]*record.Meta, n)
	for i := range n {
		metas[fmt.Sprintf("purge/%08d", i)] = &record.Meta{Created: now, Modified: now}
	}
	q := query.New("test:purge/").MustBeValid()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		// Reset the records to live ones, as the purge changes them.
		b.StopTimer()
		putRawRecords(b, bb, metas)
		b.StartTimer()

		purged, err := bb.Purge(b.Context(), q, true, true, shadowDelete)
		if err != nil {
			b.Fatal(err)
		}
		if purged != n {
			b.Fatalf("purged %d of %d records", purged, n)
		}
	}
}
