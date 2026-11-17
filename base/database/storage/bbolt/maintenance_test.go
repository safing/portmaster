package bbolt

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.etcd.io/bbolt"

	"github.com/safing/portmaster/base/database/record"
)

// Record kinds used by the maintenance tests.
const (
	kindLive = iota
	kindLiveExpiring
	kindExpired
	kindDeletedRecently
	kindDeletedLongAgo
	numKinds
)

// maintenanceTestMeta returns the meta data for a record of the given kind.
func maintenanceTestMeta(kind int, now int64) *record.Meta {
	m := &record.Meta{Created: now - 86400, Modified: now - 86400}
	switch kind {
	case kindLive:
	case kindLiveExpiring:
		m.Expires = now + 3600
	case kindExpired:
		// Expired after the purge threshold (now-60), so that the shadow deleted
		// record is not purged by a later run and maintenance is idempotent.
		m.Expires = now - 30
	case kindDeletedRecently:
		m.Deleted = now - 10
	case kindDeletedLongAgo:
		m.Deleted = now - 7200
	}
	return m
}

// putRawRecords writes records with the given meta data directly into the
// bucket, bypassing Put, so that meta data is stored as given.
func putRawRecords(tb testing.TB, b *BBolt, metas map[string]*record.Meta) {
	tb.Helper()

	err := b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		for key, meta := range metas {
			r := &TestRecord{S: key}
			r.SetKey("test:" + key)
			r.SetMeta(meta)
			data, err := r.MarshalRecord(r)
			if err != nil {
				return err
			}
			err = bucket.Put([]byte(key), data)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
}

// readAllMetas returns the meta data of all records in the bucket.
func readAllMetas(tb testing.TB, b *BBolt) map[string]record.Meta {
	tb.Helper()

	metas := make(map[string]record.Meta)
	err := b.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketName).ForEach(func(k, v []byte) error {
			w, err := record.NewRawWrapper(b.name, string(k), v)
			if err != nil {
				return err
			}
			metas[string(k)] = *w.Meta()
			return nil
		})
	})
	if err != nil {
		tb.Fatal(err)
	}
	return metas
}

// expectedAfterMaintenance returns the expected meta data of a record after
// MaintainRecordStates, and whether the record is expected to still exist.
func expectedAfterMaintenance(m record.Meta, now, purgeThreshold int64, shadowDelete bool) (record.Meta, bool) {
	switch {
	case m.Deleted == 0 && m.Expires > 0 && m.Expires < now:
		if shadowDelete {
			m.Deleted = m.Expires
			return m, true
		}
		return m, false
	case m.Deleted > 0 && (!shadowDelete || m.Deleted < purgeThreshold):
		return m, false
	default:
		return m, true
	}
}

// newMixedRecords creates n records of randomly interleaved kinds.
func newMixedRecords(n int, now int64, seed int64) map[string]*record.Meta {
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // Deterministic test data.
	metas := make(map[string]*record.Meta, n)
	for i := range n {
		metas[fmt.Sprintf("rec/%06d", i)] = maintenanceTestMeta(rng.Intn(numKinds), now)
	}
	return metas
}

// checkMaintained compares the database content with the expected outcome of
// maintaining the given original records. If allowUnprocessed is set, records
// may also still be in their original state (incomplete maintenance). It
// returns the number of records in maintained and in original state that
// differ between the two (ie. needed maintenance).
func checkMaintained(t *testing.T, b *BBolt, original map[string]*record.Meta, now, purgeThreshold int64, shadowDelete, allowUnprocessed bool) (processed, unprocessed int) {
	t.Helper()

	got := readAllMetas(t, b)
	var mismatches int
	for key, orig := range original {
		want, wantExists := expectedAfterMaintenance(*orig, now, purgeThreshold, shadowDelete)
		gotMeta, gotExists := got[key]
		maintained := gotExists == wantExists && (!wantExists || gotMeta == want)
		untouched := gotExists && gotMeta == *orig
		needsMaintenance := !wantExists || want != *orig

		switch {
		case maintained:
			if needsMaintenance {
				processed++
			}
		case allowUnprocessed && untouched:
			unprocessed++
		default:
			mismatches++
			if mismatches > 5 {
				continue
			}
			t.Errorf("record %s: got exists=%v meta=%+v, want exists=%v meta=%+v (original %+v)",
				key, gotExists, gotMeta, wantExists, want, *orig)
		}
	}
	if mismatches > 0 {
		t.Errorf("%d of %d records are not maintained correctly", mismatches, len(original))
	}
	for key := range got {
		if _, ok := original[key]; !ok && !strings.HasPrefix(key, "writer/") {
			t.Errorf("database contains unexpected record %s", key)
		}
	}
	return processed, unprocessed
}

// TestMaintainRecordStatesCorrectness runs maintenance over a few thousand
// records with randomly interleaved expired, deleted and live records and
// compares the result with the expected per-record outcome. This catches
// records skipped because the bucket was modified during cursor iteration.
func TestMaintainRecordStatesCorrectness(t *testing.T) {
	t.Parallel()

	for _, shadowDelete := range []bool{true, false} {
		t.Run(fmt.Sprintf("shadowDelete=%v", shadowDelete), func(t *testing.T) {
			t.Parallel()

			b := newPutManyTestDB(t)
			now := time.Now().Unix()
			purgeDeletedBefore := time.Unix(now-60, 0)
			original := newMixedRecords(5000, now, 1)
			putRawRecords(t, b, original)

			err := b.MaintainRecordStates(t.Context(), purgeDeletedBefore, shadowDelete)
			if err != nil {
				t.Fatal(err)
			}
			checkMaintained(t, b, original, now, purgeDeletedBefore.Unix(), shadowDelete, false)

			// Maintenance is idempotent: a second run must not change anything.
			err = b.MaintainRecordStates(t.Context(), purgeDeletedBefore, shadowDelete)
			if err != nil {
				t.Fatal(err)
			}
			checkMaintained(t, b, original, now, purgeDeletedBefore.Unix(), shadowDelete, false)
		})
	}
}

// TestMaintainRecordStatesShadowDeleteThenPurge is the minimal case of a
// record being skipped: with shadow delete, an expired record is rewritten
// (Put + Seek, which leaves the cursor on a materialized node), the next record
// is purged with Cursor.Delete, which shifts the node's elements so that the
// following Cursor.Next skipped the record after it.
func TestMaintainRecordStatesShadowDeleteThenPurge(t *testing.T) {
	t.Parallel()

	b := newPutManyTestDB(t)
	now := time.Now().Unix()
	purgeDeletedBefore := time.Unix(now-60, 0)
	original := map[string]*record.Meta{
		"a": maintenanceTestMeta(kindExpired, now),
		"b": maintenanceTestMeta(kindDeletedLongAgo, now),
		"c": maintenanceTestMeta(kindDeletedLongAgo, now),
		"d": maintenanceTestMeta(kindLive, now),
	}
	putRawRecords(t, b, original)

	err := b.MaintainRecordStates(t.Context(), purgeDeletedBefore, true)
	if err != nil {
		t.Fatal(err)
	}
	checkMaintained(t, b, original, now, purgeDeletedBefore.Unix(), true, false)
}

// cancelAfterContext is a context that cancels itself on the n-th time its
// cancellation state is checked (via Done or Err), so that maintenance is
// cancelled in the middle of its walk independent of timing.
type cancelAfterContext struct {
	context.Context //nolint:containedctx // Wrapped for the test.

	cancel context.CancelFunc
	checks atomic.Int64
	n      int64
}

func newCancelAfterContext(tb testing.TB, n int64) *cancelAfterContext {
	tb.Helper()

	ctx, cancel := context.WithCancel(tb.Context())
	tb.Cleanup(cancel)
	return &cancelAfterContext{Context: ctx, cancel: cancel, n: n}
}

func (c *cancelAfterContext) Done() <-chan struct{} {
	c.check()
	return c.Context.Done()
}

func (c *cancelAfterContext) Err() error {
	c.check()
	return c.Context.Err()
}

func (c *cancelAfterContext) check() {
	if c.checks.Add(1) >= c.n {
		c.cancel()
	}
}

// TestMaintainRecordStatesCancel checks that a cancelled maintenance reports
// the cancellation, that the already processed records are consistently
// maintained, and that a later run completes the maintenance.
// This test changes the chunk size and must not run in parallel.
func TestMaintainRecordStatesCancel(t *testing.T) { //nolint:paralleltest // Changes the chunk size.
	// Use small chunks in order to keep the test fast.
	defer func(size int) {
		maintenanceChunkSize = size
	}(maintenanceChunkSize)
	maintenanceChunkSize = 1000

	b := newPutManyTestDB(t)
	now := time.Now().Unix()
	purgeDeletedBefore := time.Unix(now-60, 0)
	original := newMixedRecords(10000, now, 2)
	putRawRecords(t, b, original)

	// Already cancelled: nothing may be done and the caller must be told.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := b.MaintainRecordStates(ctx, purgeDeletedBefore, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled for cancelled context, got %v", err)
	}
	processed, _ := checkMaintained(t, b, original, now, purgeDeletedBefore.Unix(), true, true)
	if processed != 0 {
		t.Fatalf("%d records were maintained with a cancelled context", processed)
	}

	// Cancelled in the middle of the walk.
	err = b.MaintainRecordStates(newCancelAfterContext(t, 3), purgeDeletedBefore, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled for maintenance cancelled mid-walk, got %v", err)
	}
	processed, unprocessed := checkMaintained(t, b, original, now, purgeDeletedBefore.Unix(), true, true)
	t.Logf("cancelled mid-walk: %d records maintained, %d left for later", processed, unprocessed)
	if unprocessed == 0 {
		t.Fatal("maintenance was not cancelled mid-walk")
	}

	// A later run completes the maintenance.
	err = b.MaintainRecordStates(t.Context(), purgeDeletedBefore, true)
	if err != nil {
		t.Fatal(err)
	}
	checkMaintained(t, b, original, now, purgeDeletedBefore.Unix(), true, false)
}

// TestMaintainRecordStatesDoesNotBlockWriters checks that other writers of the
// database can write while the maintenance walks a large bucket, instead of
// being blocked for the whole walk.
// This test changes the chunk size and must not run in parallel.
func TestMaintainRecordStatesDoesNotBlockWriters(t *testing.T) { //nolint:paralleltest // Changes the chunk size.
	// Use small chunks in order to keep the test fast.
	defer func(size int) {
		maintenanceChunkSize = size
	}(maintenanceChunkSize)
	maintenanceChunkSize = 1000

	b := newPutManyTestDB(t)
	now := time.Now().Unix()
	n := 20 * maintenanceChunkSize
	original := make(map[string]*record.Meta, n)
	for i := range n {
		// Every record needs to be rewritten, making the walk long.
		original[fmt.Sprintf("rec/%06d", i)] = maintenanceTestMeta(kindExpired, now)
	}
	putRawRecords(t, b, original)

	type write struct{ start, end time.Time }
	var (
		wg     sync.WaitGroup
		stop   = make(chan struct{})
		writes []write
	)
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			start := time.Now()
			_, err := b.Put(newPutManyTestRecord(fmt.Sprintf("writer/%d", i)))
			if err != nil {
				t.Error(err)
				return
			}
			writes = append(writes, write{start: start, end: time.Now()})
			time.Sleep(100 * time.Microsecond)
		}
	})

	maintStart := time.Now()
	err := b.MaintainRecordStates(t.Context(), time.Unix(now-60, 0), true)
	maintEnd := time.Now()
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}

	// Only count writes that were done entirely while the maintenance was
	// running, after it had time to take the write lock.
	var (
		putsDuring int
		maxLatency time.Duration
	)
	for _, w := range writes {
		maxLatency = max(maxLatency, w.end.Sub(w.start))
		if w.start.After(maintStart.Add(time.Millisecond)) && w.end.Before(maintEnd) {
			putsDuring++
		}
	}

	t.Logf("maintenance took %s, max writer latency %s, %d writes completed during maintenance",
		maintEnd.Sub(maintStart), maxLatency, putsDuring)
	// Without chunking, no write could be done during the walk and the
	// latency of a write was the duration of the whole walk.
	if putsDuring < 3 || maxLatency > maintEnd.Sub(maintStart)/2 {
		t.Fatalf("writers were blocked during maintenance: only %d writes completed during the walk of %s (max writer latency %s)",
			putsDuring, maintEnd.Sub(maintStart), maxLatency)
	}
	checkMaintained(t, b, original, now, now-60, true, false)
}

// BenchmarkMaintainRecordStates measures the total time of maintaining 50k
// records, with a mix of records that need to be changed and with records
// that are all up to date.
func BenchmarkMaintainRecordStates(b *testing.B) {
	now := time.Now().Unix()
	purgeDeletedBefore := time.Unix(now-60, 0)
	unchanged := make(map[string]*record.Meta, 50000)
	for i := range 50000 {
		unchanged[fmt.Sprintf("rec/%06d", i)] = maintenanceTestMeta(kindLiveExpiring, now)
	}

	for _, bc := range []struct {
		name    string
		records map[string]*record.Meta
	}{
		{name: "mixed", records: newMixedRecords(50000, now, 3)},
		{name: "unchanged", records: unchanged},
	} {
		b.Run(bc.name, func(b *testing.B) {
			db, err := NewBBolt("test", b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer func() {
				_ = db.Shutdown()
			}()
			bb := db.(*BBolt) //nolint:forcetypeassert // Test.

			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				putRawRecords(b, bb, bc.records)
				b.StartTimer()

				err = bb.MaintainRecordStates(context.Background(), purgeDeletedBefore, true)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
