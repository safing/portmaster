package database

import (
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/database/record"
)

type Example struct {
	record.Base
	sync.Mutex

	Name  string
	Score int
}

var exampleDB = NewInterface(&Options{
	Internal: true,
	Local:    true,
})

// GetExample gets an Example from the database.
func GetExample(key string) (*Example, error) {
	r, err := exampleDB.Get(key)
	if err != nil {
		return nil, err
	}

	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newExample := &Example{}
		err = record.Unwrap(r, newExample)
		if err != nil {
			return nil, err
		}
		return newExample, nil
	}

	// or adjust type
	newExample, ok := r.(*Example)
	if !ok {
		return nil, fmt.Errorf("record not of type *Example, but %T", r)
	}
	return newExample, nil
}

func (e *Example) Save() error {
	return exampleDB.Put(e)
}

func (e *Example) SaveAs(key string) error {
	e.SetKey(key)
	return exampleDB.PutNew(e)
}

func NewExample(key, name string, score int) *Example {
	newExample := &Example{
		Name:  name,
		Score: score,
	}
	newExample.SetKey(key)
	return newExample
}
