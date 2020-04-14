package filterlists

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates"
	"golang.org/x/sync/errgroup"
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
const filterlistsDisabled = "filterlists:disabled"

var (
	filterListLock sync.RWMutex

	// Updater files for tracking upgrades.
	baseFile         *updater.File
	intermediateFile *updater.File
	urgentFile       *updater.File

	filterListsLoaded chan struct{}
)

var (
	cache = database.NewInterface(&database.Options{
		CacheSize: 2 ^ 8,
	})
)

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

// processListFile opens the latest version of f	ile and decodes it's DSDL
// content. It calls processEntry for each decoded filterlists entry.
func processListFile(ctx context.Context, filter *scopedBloom, file *updater.File) error {
	f, err := os.Open(file.Path())
	if err != nil {
		return err
	}
	defer f.Close()

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

	var cnt int
	start := time.Now()

	batch := database.NewInterface(&database.Options{Local: true, Internal: true})
	var startBatch func()
	processBatch := func() error {
		batchPut := batch.PutMany("cache")
		for r := range records {
			if err := batchPut(r); err != nil {
				return err
			}
			cnt++

			if cnt%10000 == 0 {
				timePerEntity := time.Since(start) / time.Duration(cnt)
				speed := float64(time.Second) / float64(timePerEntity)
				log.Debugf("processed %d entities %s with %s / entity (%.2f entits/second)", cnt, time.Since(start), timePerEntity, speed)
			}

			if cnt%1000 == 0 {
				if err := batchPut(nil); err != nil {
					return err
				}

				startBatch()

				return nil
			}
		}

		return batchPut(nil)
	}
	startBatch = func() {
		startSafe(processBatch)
	}

	startBatch()

	return g.Wait()
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

	if len(entry.Sources) > 0 {
		filter.add(entry.Type, entry.Entity)
	}

	r := &entityRecord{
		Value:     entry.Entity,
		Type:      entry.Type,
		Sources:   entry.Sources,
		UpdatedAt: time.Now().Unix(),
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
