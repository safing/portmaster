package database

import (
	"errors"
	"fmt"
	"testing"
)

// TestDeleteWithPendingDelayedWrite checks that a Delete wins over an earlier,
// not yet flushed delayed write of the same key.
func TestDeleteWithPendingDelayedWrite(t *testing.T) { //nolint:paralleltest // Subtests register databases and share the registry.
	for _, storageType := range []string{"hashmap", "bbolt", "sqlite"} { //nolint:goconst,paralleltest // Storage type names; subtests share the registry.
		for _, shadowDelete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_%v", storageType, shadowDelete), func(t *testing.T) {
				dbName := fmt.Sprintf("test-delete-pending-%s-%v", storageType, shadowDelete)
				_, err := Register(&Database{
					Name:         dbName,
					Description:  "Unit Test Database for Delete with delayed writes",
					StorageType:  storageType,
					ShadowDelete: shadowDelete,
				})
				if err != nil {
					t.Fatal(err)
				}

				cachingDB := NewInterface(&Options{
					Local:             true,
					Internal:          true,
					CacheSize:         100,
					DelayCachedWrites: dbName,
				})
				plainDB := NewInterface(&Options{
					Local:    true,
					Internal: true,
				})

				key := dbName + ":deleted"
				err = cachingDB.Put(NewExample(key, "A", 1))
				if err != nil {
					t.Fatal(err)
				}
				err = cachingDB.Delete(key)
				if err != nil {
					t.Fatal(err)
				}

				// The deleted record must not be served from the cache.
				_, err = cachingDB.Get(key)
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("deleted record still served by caching interface (err=%v)", err)
				}

				// The pending write must have been dropped.
				cachingDB.writeCacheLock.Lock()
				_, pending := cachingDB.writeCache[key]
				cachingDB.writeCacheLock.Unlock()
				if pending {
					t.Fatal("deleted record still pending in write cache")
				}

				// Flushing the write cache must not resurrect the record.
				cachingDB.flushWriteCache(0)
				_, err = plainDB.Get(key)
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("deleted record present in storage after flush (err=%v)", err)
				}
				_, err = cachingDB.Get(key)
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("deleted record served by caching interface after flush (err=%v)", err)
				}
			})
		}
	}
}
