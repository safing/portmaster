package database

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/safing/portmaster/service/mgr"
)

func benchmarkCacheWriting(b *testing.B, storageType string, cacheSize int, sampleSize int, delayWrites bool) { //nolint:gocognit,gocyclo,thelper
	b.Run(fmt.Sprintf("CacheWriting_%s_%d_%d_%v", storageType, cacheSize, sampleSize, delayWrites), func(b *testing.B) {
		// Setup Benchmark.

		// Create database.
		dbName := fmt.Sprintf("cache-w-benchmark-%s-%d-%d-%v", storageType, cacheSize, sampleSize, delayWrites)
		_, err := Register(&Database{
			Name:        dbName,
			Description: fmt.Sprintf("Cache Benchmark Database for %s", storageType),
			StorageType: storageType,
		})
		if err != nil {
			b.Fatal(err)
		}

		// Create benchmark interface.
		options := &Options{
			Local:     true,
			Internal:  true,
			CacheSize: cacheSize,
		}
		if cacheSize > 0 && delayWrites {
			options.DelayCachedWrites = dbName
		}
		db := NewInterface(options)

		// Start
		m := mgr.New("Cache writing benchmark test")
		var wg sync.WaitGroup
		if cacheSize > 0 && delayWrites {
			wg.Add(1)
			m.Go("Cache writing benchmark worker", func(wc *mgr.WorkerCtx) error {
				err := db.DelayedCacheWriter(wc)
				if err != nil {
					panic(err)
				}
				wg.Done()
				return nil
			})
		}

		// Start Benchmark.
		b.ResetTimer()
		for i := range b.N {
			testRecordID := i % sampleSize
			r := NewExample(
				dbName+":"+strconv.Itoa(testRecordID),
				"A",
				1,
			)
			err = db.Put(r)
			if err != nil {
				b.Fatal(err)
			}
		}

		// End cache writer and wait
		m.Cancel()
		wg.Wait()
	})
}

func benchmarkCacheReadWrite(b *testing.B, storageType string, cacheSize int, sampleSize int, delayWrites bool) { //nolint:gocognit,gocyclo,thelper
	b.Run(fmt.Sprintf("CacheReadWrite_%s_%d_%d_%v", storageType, cacheSize, sampleSize, delayWrites), func(b *testing.B) {
		// Setup Benchmark.

		// Create database.
		dbName := fmt.Sprintf("cache-rw-benchmark-%s-%d-%d-%v", storageType, cacheSize, sampleSize, delayWrites)
		_, err := Register(&Database{
			Name:        dbName,
			Description: fmt.Sprintf("Cache Benchmark Database for %s", storageType),
			StorageType: storageType,
		})
		if err != nil {
			b.Fatal(err)
		}

		// Create benchmark interface.
		options := &Options{
			Local:     true,
			Internal:  true,
			CacheSize: cacheSize,
		}
		if cacheSize > 0 && delayWrites {
			options.DelayCachedWrites = dbName
		}
		db := NewInterface(options)

		// Start
		m := mgr.New("Cache read/write benchmark test")
		var wg sync.WaitGroup
		if cacheSize > 0 && delayWrites {
			wg.Add(1)
			m.Go("Cache read/write benchmark worker", func(wc *mgr.WorkerCtx) error {
				err := db.DelayedCacheWriter(wc)
				if err != nil {
					panic(err)
				}
				wg.Done()
				return nil
			})
		}

		// Start Benchmark.
		b.ResetTimer()
		writing := true
		for i := range b.N {
			testRecordID := i % sampleSize
			key := dbName + ":" + strconv.Itoa(testRecordID)

			if i > 0 && testRecordID == 0 {
				writing = !writing // switch between reading and writing every samplesize
			}

			if writing {
				r := NewExample(key, "A", 1)
				err = db.Put(r)
			} else {
				_, err = db.Get(key)
			}
			if err != nil {
				b.Fatal(err)
			}
		}

		// End cache writer and wait
		m.Cancel()
		wg.Wait()
	})
}

func BenchmarkCache(b *testing.B) {
	for _, storageType := range []string{"bbolt", "hashmap"} {
		benchmarkCacheWriting(b, storageType, 32, 8, false)
		benchmarkCacheWriting(b, storageType, 32, 8, true)
		benchmarkCacheWriting(b, storageType, 32, 1024, false)
		benchmarkCacheWriting(b, storageType, 32, 1024, true)
		benchmarkCacheWriting(b, storageType, 512, 1024, false)
		benchmarkCacheWriting(b, storageType, 512, 1024, true)

		benchmarkCacheReadWrite(b, storageType, 32, 8, false)
		benchmarkCacheReadWrite(b, storageType, 32, 8, true)
		benchmarkCacheReadWrite(b, storageType, 32, 1024, false)
		benchmarkCacheReadWrite(b, storageType, 32, 1024, true)
		benchmarkCacheReadWrite(b, storageType, 512, 1024, false)
		benchmarkCacheReadWrite(b, storageType, 512, 1024, true)
	}
}
