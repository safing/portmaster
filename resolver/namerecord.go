package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
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
		new := &NameRecord{}
		err = record.Unwrap(r, new)
		if err != nil {
			return nil, err
		}
		// Check if the record is valid.
		if !new.IsValid() {
			return nil, errors.New("record is invalid (outdated format)")
		}

		return new, nil
	}

	// Or just adjust the type.
	new, ok := r.(*NameRecord)
	if !ok {
		return nil, fmt.Errorf("record not of type *NameRecord, but %T", r)
	}
	// Check if the record is valid.
	if !new.IsValid() {
		return nil, errors.New("record is invalid (outdated format)")
	}

	return new, nil
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
func (rec *NameRecord) Save() error {
	if rec.Domain == "" || rec.Question == "" {
		return errors.New("could not save NameRecord, missing Domain and/or Question")
	}

	rec.SetKey(makeNameRecordKey(rec.Domain, rec.Question))
	rec.UpdateMeta()
	rec.Meta().SetAbsoluteExpiry(rec.Expires + databaseOvertime)

	return recordDatabase.PutNew(rec)
}

// clearNameCache clears all dns caches from the database.
func clearNameCache(ar *api.Request) (msg string, err error) {
	log.Info("resolver: user requested dns cache clearing via action")

	recordDatabase.FlushCache()
	recordDatabase.ClearCache()
	n, err := recordDatabase.Purge(ar.Context(), query.New(nameRecordsKeyPrefix))
	if err != nil {
		return "", err
	}

	log.Debugf("resolver: cleared %d entries from dns cache", n)
	return fmt.Sprintf("cleared %d dns cache entries", n), nil
}

// DEPRECATED: remove in v0.7
func clearNameCacheEventHandler(ctx context.Context, _ interface{}) error {
	log.Debugf("resolver: dns cache clearing started...")

	recordDatabase.FlushCache()
	recordDatabase.ClearCache()
	n, err := recordDatabase.Purge(ctx, query.New(nameRecordsKeyPrefix))
	if err != nil {
		return err
	}

	log.Debugf("resolver: cleared %d entries from dns cache", n)
	return nil
}
