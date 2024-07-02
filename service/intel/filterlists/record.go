package filterlists

import (
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/database/record"
)

type entityRecord struct {
	record.Base `json:"-"`
	sync.Mutex  `json:"-"`

	Value     string
	Sources   []string
	Type      string
	UpdatedAt int64
}

func getEntityRecordByKey(key string) (*entityRecord, error) {
	r, err := cache.Get(key)
	if err != nil {
		return nil, err
	}

	if r.IsWrapped() {
		newER := &entityRecord{}
		if err := record.Unwrap(r, newER); err != nil {
			return nil, err
		}

		return newER, nil
	}

	newER, ok := r.(*entityRecord)
	if !ok {
		return nil, fmt.Errorf("record not of type *entityRecord, but %T", r)
	}
	return newER, nil
}
