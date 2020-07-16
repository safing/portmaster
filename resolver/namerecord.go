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

var (
	recordDatabase = database.NewInterface(&database.Options{
		AlwaysSetRelativateExpiry: 2592000, // 30 days
		CacheSize:                 256,
	})

	nameRecordsKeyPrefix = "cache:intel/nameRecord/"
)

// NameRecord is helper struct to RRCache to better save data to the database.
type NameRecord struct {
	record.Base
	sync.Mutex

	Domain   string
	Question string
	Answer   []string
	Ns       []string
	Extra    []string
	TTL      int64

	Server      string
	ServerScope int8
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
	return recordDatabase.PutNew(rec)
}

func clearNameCache(_ context.Context, _ interface{}) error {
	log.Debugf("resolver: name cache clearing started...")
	for {
		done, err := removeNameEntries(10000)
		if err != nil {
			return err
		}

		if done {
			return nil
		}
	}
}

func removeNameEntries(batchSize int) (bool, error) {
	iter, err := recordDatabase.Query(query.New(nameRecordsKeyPrefix))
	if err != nil {
		return false, err
	}

	keys := make([]string, 0, batchSize)

	var cnt int
	for r := range iter.Next {
		cnt++
		keys = append(keys, r.Key())

		if cnt == batchSize {
			break
		}
	}
	iter.Cancel()

	for _, key := range keys {
		if err := recordDatabase.Delete(key); err != nil {
			log.Warningf("resolver: failed to remove name cache entry %q: %s", key, err)
		}
	}

	log.Debugf("resolver: successfully removed %d name cache entries", cnt)

	// if we removed less entries that the batch size we
	// are done and no more entries exist
	return cnt < batchSize, nil
}
