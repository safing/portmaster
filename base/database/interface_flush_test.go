package database

import "testing"

func TestFlushCache(t *testing.T) {
	t.Parallel()

	dbName := "test-flush-cache"
	_, err := Register(&Database{
		Name:        dbName,
		Description: "Unit Test Database for FlushCache",
		StorageType: "hashmap", //nolint:goconst // Storage type name.
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

	key := dbName + ":flushed"
	err = cachingDB.Put(NewExample(key, "A", 1))
	if err != nil {
		t.Fatal(err)
	}

	// The write must still be pending in the write cache.
	_, err = plainDB.Get(key)
	if err == nil {
		t.Fatal("record was written to storage before flush")
	}

	cachingDB.FlushCache()

	_, err = plainDB.Get(key)
	if err != nil {
		t.Fatalf("record not in storage after FlushCache: %s", err)
	}
	cachingDB.writeCacheLock.Lock()
	pending := len(cachingDB.writeCache)
	cachingDB.writeCacheLock.Unlock()
	if pending != 0 {
		t.Fatalf("write cache not empty after FlushCache: %d entries", pending)
	}

	// FlushCache must be a safe no-op without a cache or delayed writes.
	plainDB.FlushCache()
	NewInterface(&Options{Local: true, Internal: true, CacheSize: 10}).FlushCache()
}
