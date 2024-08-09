package filterlists

import (
	"fmt"
	"sync"

	"github.com/hashicorp/go-version"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
)

const resetVersion = "v0.6.0"

type cacheVersionRecord struct {
	record.Base
	sync.Mutex

	Version string
	Reset   string
}

// getCacheDatabaseVersion reads and returns the cache
// database version record.
func getCacheDatabaseVersion() (*version.Version, error) {
	r, err := cache.Get(filterListCacheVersionKey)
	if err != nil {
		return nil, err
	}

	var verRecord *cacheVersionRecord
	if r.IsWrapped() {
		verRecord = new(cacheVersionRecord)
		if err := record.Unwrap(r, verRecord); err != nil {
			return nil, err
		}
	} else {
		var ok bool
		verRecord, ok = r.(*cacheVersionRecord)
		if !ok {
			return nil, fmt.Errorf("invalid type, expected cacheVersionRecord but got %T", r)
		}
	}

	if verRecord.Reset != resetVersion {
		return nil, database.ErrNotFound
	}

	ver, err := version.NewSemver(verRecord.Version)
	if err != nil {
		return nil, err
	}

	return ver, nil
}

// setCacheDatabaseVersion updates the cache database
// version record to ver.
func setCacheDatabaseVersion(ver string) error {
	verRecord := &cacheVersionRecord{
		Version: ver,
		Reset:   resetVersion,
	}

	verRecord.SetKey(filterListCacheVersionKey)
	return cache.Put(verRecord)
}
