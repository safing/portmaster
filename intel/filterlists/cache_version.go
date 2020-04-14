package filterlists

import (
	"fmt"
	"sync"

	"github.com/hashicorp/go-version"
	"github.com/safing/portbase/database/record"
)

type cacheVersionRecord struct {
	record.Base
	sync.Mutex

	Version string
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
	}

	verRecord.SetKey(filterListCacheVersionKey)
	return cache.Put(verRecord)
}
