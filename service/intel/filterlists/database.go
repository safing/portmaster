package filterlists

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/service/updates"
)

const (
	baseListFilePath         = "intel/lists/base.dsdl"
	intermediateListFilePath = "intel/lists/intermediate.dsdl"
	urgentListFilePath       = "intel/lists/urgent.dsdl"
	listIndexFilePath        = "intel/lists/index.dsd"
)

// default bloomfilter element sizes (estimated).
const (
	domainBfSize  = 1000000
	asnBfSize     = 1000
	countryBfSize = 100
	ipv4BfSize    = 100
	ipv6BfSize    = 100
)

const bfFalsePositiveRate = 0.001

var (
	filterListLock sync.RWMutex

	// Updater files for tracking upgrades.
	baseFile         *updater.File
	intermediateFile *updater.File
	urgentFile       *updater.File

	filterListsLoaded chan struct{}
)

var cache = database.NewInterface(&database.Options{
	Local:     true,
	Internal:  true,
	CacheSize: 256,
})

// getFileFunc is the function used to get a file from
// the updater. It's basically updates.GetFile and used
// for unit testing.
type getFileFunc func(string) (*updater.File, error)

// getFile points to updates.GetFile but may be set to
// something different during unit testing.
var getFile getFileFunc = updates.GetFile

func init() {
	filterListsLoaded = make(chan struct{})
}

// isLoaded returns true if the filterlists have been
// loaded.
func isLoaded() bool {
	select {
	case <-filterListsLoaded:
		return true
	default:
		return false
	}
}

// processListFile opens the latest version of file and decodes it's DSDL
// content. It calls processEntry for each decoded filterlists entry.
func processListFile(ctx context.Context, filter *scopedBloom, file *updater.File) error {
	f, err := os.Open(file.Path())
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	values := make(chan *listEntry, 100)
	records := make(chan record.Record, 100)

	g, ctx := errgroup.WithContext(ctx)

	// startSafe runs fn inside the error group but wrapped
	// in recovered function.
	startSafe := func(fn func() error) {
		g.Go(func() (err error) {
			defer func() {
				if x := recover(); x != nil {
					if e, ok := x.(error); ok {
						err = e
					} else {
						err = fmt.Errorf("%v", x)
					}
				}
			}()

			err = fn()
			return err
		})
	}

	startSafe(func() (err error) {
		defer close(values)

		err = decodeFile(ctx, f, values)
		return
	})

	startSafe(func() error {
		defer close(records)
		for entry := range values {
			if err := processEntry(ctx, filter, entry, records); err != nil {
				return err
			}
		}

		return nil
	})

	persistRecords(startSafe, records)

	return g.Wait()
}

func persistRecords(startJob func(func() error), records <-chan record.Record) {
	var cnt int
	start := time.Now()
	logProgress := func() {
		if cnt == 0 {
			// protection against panic
			return
		}

		timePerEntity := time.Since(start) / time.Duration(cnt)
		speed := float64(time.Second) / float64(timePerEntity)
		log.Debugf("processed %d entities in %s with %s / entity (%.2f entities/second)", cnt, time.Since(start), timePerEntity, speed)
	}

	batch := database.NewInterface(&database.Options{Local: true, Internal: true})

	var processBatch func() error
	processBatch = func() error {
		batchPut := batch.PutMany("cache")
		for r := range records {
			if err := batchPut(r); err != nil {
				return err
			}
			cnt++

			if cnt%10000 == 0 {
				logProgress()
			}

			if cnt%1000 == 0 {
				if err := batchPut(nil); err != nil {
					return err
				}

				startJob(processBatch)

				return nil
			}
		}

		// log final batch
		if cnt%10000 != 0 { // avoid duplicate logging
			logProgress()
		}
		return batchPut(nil)
	}

	startJob(processBatch)
}

func normalizeEntry(entry *listEntry) {
	switch strings.ToLower(entry.Type) { //
	case "domain":
		entry.Entity = strings.ToLower(entry.Entity)
		if entry.Entity[len(entry.Entity)-1] != '.' {
			// ensure domains from the filter list are fully qualified and end in dot.
			entry.Entity += "."
		}
	default:
	}
}

func processEntry(ctx context.Context, filter *scopedBloom, entry *listEntry, records chan<- record.Record) error {
	normalizeEntry(entry)

	// Only add the entry to the bloom filter if it has any sources.
	if len(entry.Resources) > 0 {
		filter.add(entry.Type, entry.Entity)
	}

	r := &entityRecord{
		Value:     entry.Entity,
		Type:      entry.Type,
		Sources:   entry.getSources(),
		UpdatedAt: time.Now().Unix(),
	}

	// If the entry is a "delete" update, actually delete it to save space.
	if entry.Whitelist {
		r.CreateMeta()
		r.Meta().Delete()
	}

	key := makeListCacheKey(strings.ToLower(r.Type), r.Value)
	r.SetKey(key)

	select {
	case records <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func mapKeys(m map[string]struct{}) []string {
	sl := make([]string, 0, len(m))
	for s := range m {
		sl = append(sl, s)
	}

	sort.Strings(sl)
	return sl
}
