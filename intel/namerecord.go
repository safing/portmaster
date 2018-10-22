package intel

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/record"
)

var (
	recordDatabase = database.NewInterface(&database.Options{
		AlwaysSetRelativateExpiry: 2592000, // 30 days
		CacheSize:                 100,
	})
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
}

func makeNameRecordKey(domain string, question string) string {
	return fmt.Sprintf("intel:NameRecords/%s%s", domain, question)
}

// GetNameRecord gets a NameRecord from the database.
func GetNameRecord(domain string, question string) (*NameRecord, error) {
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

// Save saves the NameRecord to the database.
func (rec *NameRecord) Save() error {
	if rec.Domain == "" || rec.Question == "" {
		return errors.New("could not save NameRecord, missing Domain and/or Question")
	}

	rec.SetKey(makeNameRecordKey(rec.Domain, rec.Question))
	return recordDatabase.PutNew(rec)
}
