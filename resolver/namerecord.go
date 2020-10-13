package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
	// TODO: Name change in progress. Rename "TTL" field to "Expires" in Q1 2021.
	TTL int64 `json:"Expires"`

	Server      string
	ServerScope int8
	ServerInfo  string
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

	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		new := &NameRecord{}
		err = record.Unwrap(r, new)
		if err != nil {
			return nil, err
		}
		return new, nil
	}

	// or adjust type
	new, ok := r.(*NameRecord)
	if !ok {
		return nil, fmt.Errorf("record not of type *NameRecord, but %T", r)
	}
	return new, nil
}

// DeleteNameRecord deletes a NameRecord from the database.
func DeleteNameRecord(domain, question string) error {
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
	rec.Meta().SetAbsoluteExpiry(rec.TTL + databaseOvertime)

	return recordDatabase.PutNew(rec)
}

func clearNameCache(ctx context.Context, _ interface{}) error {
	log.Debugf("resolver: dns cache clearing started...")
	n, err := recordDatabase.Purge(ctx, query.New(nameRecordsKeyPrefix))
	if err != nil {
		return err
	}

	log.Debugf("resolver: cleared %d entries in dns cache", n)
	return nil
}
