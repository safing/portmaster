package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
)

const (
	// databaseOvertime defines how much longer than the TTL name records are
	// cached in the database.
	databaseOvertime = 86400 * 14 // two weeks
)

var (
	recordDatabase = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,

		// Cache entries because application often resolve domains multiple times.
		CacheSize: 256,

		// We only use the cache database here, so we can delay and batch all our
		// writes. Also, no one else accesses these records, so we are fine using
		// this.
		DelayCachedWrites: "cache",
	})

	nameRecordsKeyPrefix = "cache:intel/nameRecord/"
)

// NameRecord is helper struct to RRCache to better save data to the database.
type NameRecord struct {
	record.Base
	sync.Mutex

	Domain   string
	Question string
	RCode    int
	Answer   []string
	Ns       []string
	Extra    []string
	Expires  int64

	Resolver *ResolverInfo
}

// IsValid returns whether the NameRecord is valid and may be used. Otherwise,
// it should be disregarded.
func (nameRecord *NameRecord) IsValid() bool {
	switch {
	case nameRecord.Resolver == nil || nameRecord.Resolver.Type == "":
		// Changed in v0.6.7: Introduced Resolver *ResolverInfo
		return false
	default:
		// Up to date!
		return true
	}
}

func makeNameRecordKey(domain string, question string) string {
	return nameRecordsKeyPrefix + domain + question
}

// GetNameRecord gets a NameRecord from the database.
func GetNameRecord(domain, question string) (*NameRecord, error) {
	key := makeNameRecordKey(domain, question)

	r, err := recordDatabase.Get(key)
	if err != nil {
		return nil, err
	}

	// Unwrap record if it's wrapped.
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newNR := &NameRecord{}
		err = record.Unwrap(r, newNR)
		if err != nil {
			return nil, err
		}
		// Check if the record is valid.
		if !newNR.IsValid() {
			return nil, errors.New("record is invalid (outdated format)")
		}

		return newNR, nil
	}

	// Or just adjust the type.
	newNR, ok := r.(*NameRecord)
	if !ok {
		return nil, fmt.Errorf("record not of type *NameRecord, but %T", r)
	}
	// Check if the record is valid.
	if !newNR.IsValid() {
		return nil, errors.New("record is invalid (outdated format)")
	}

	return newNR, nil
}

// ResetCachedRecord deletes a NameRecord from the cache database.
func ResetCachedRecord(domain, question string) error {
	// In order to properly delete an entry, we must also clear the caches.
	recordDatabase.FlushCache()
	recordDatabase.ClearCache()

	key := makeNameRecordKey(domain, question)
	return recordDatabase.Delete(key)
}

// Save saves the NameRecord to the database.
func (nameRecord *NameRecord) Save() error {
	if nameRecord.Domain == "" || nameRecord.Question == "" {
		return errors.New("could not save NameRecord, missing Domain and/or Question")
	}

	nameRecord.SetKey(makeNameRecordKey(nameRecord.Domain, nameRecord.Question))
	nameRecord.UpdateMeta()
	nameRecord.Meta().SetAbsoluteExpiry(nameRecord.Expires + databaseOvertime)

	return recordDatabase.PutNew(nameRecord)
}

// clearNameCacheHandler is an API handler that clears all dns caches from the database.
func clearNameCacheHandler(ar *api.Request) (msg string, err error) {
	log.Info("resolver: user requested dns cache clearing via action")

	return clearNameCache(ar.Context())
}

// clearNameCache clears all dns caches from the database.
func clearNameCache(ctx context.Context) (msg string, err error) {
	recordDatabase.FlushCache()
	recordDatabase.ClearCache()
	n, err := recordDatabase.Purge(ctx, query.New(nameRecordsKeyPrefix))
	if err != nil {
		return "", err
	}

	log.Debugf("resolver: cleared %d entries from dns cache", n)
	return fmt.Sprintf("cleared %d dns cache entries", n), nil
}
