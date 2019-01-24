// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"fmt"
	"sync"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/record"
)

var (
	intelDatabase = database.NewInterface(&database.Options{
		AlwaysSetRelativateExpiry: 2592000, // 30 days
	})
)

// Intel holds intelligence data for a domain.
type Intel struct {
	record.Base
	sync.Mutex

	Domain string
}

func makeIntelKey(domain string) string {
	return fmt.Sprintf("cache:intel/domain/%s", domain)
}

// GetIntelFromDB gets an Intel record from the database.
func GetIntelFromDB(domain string) (*Intel, error) {
	key := makeIntelKey(domain)

	r, err := intelDatabase.Get(key)
	if err != nil {
		return nil, err
	}

	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		new := &Intel{}
		err = record.Unwrap(r, new)
		if err != nil {
			return nil, err
		}
		return new, nil
	}

	// or adjust type
	new, ok := r.(*Intel)
	if !ok {
		return nil, fmt.Errorf("record not of type *Intel, but %T", r)
	}
	return new, nil
}

// Save saves the Intel record to the database.
func (intel *Intel) Save() error {
	intel.SetKey(makeIntelKey(intel.Domain))
	return intelDatabase.PutNew(intel)
}

// GetIntel fetches intelligence data for the given domain.
func GetIntel(domain string) (*Intel, error) {
	return &Intel{Domain: domain}, nil
}
