/*
Package database provides a universal interface for interacting with the database.

# A Lazy Database

The database system can handle Go structs as well as serialized data by the dsd package.
While data is in transit within the system, it does not know which form it currently has. Only when it reaches its destination, it must ensure that it is either of a certain type or dump it.

# Record Interface

The database system uses the Record interface to transparently handle all types of structs that get saved in the database. Structs include the Base struct to fulfill most parts of the Record interface.

Boilerplate Code:

	type Example struct {
	  record.Base
	  sync.Mutex

	  Name  string
	  Score int
	}

	var (
	  db = database.NewInterface(nil)
	)

	// GetExample gets an Example from the database.
	func GetExample(key string) (*Example, error) {
	  r, err := db.Get(key)
	  if err != nil {
	    return nil, err
	  }

	  // unwrap
	  if r.IsWrapped() {
	    // only allocate a new struct, if we need it
	    new := &Example{}
	    err = record.Unwrap(r, new)
	    if err != nil {
	      return nil, err
	    }
	    return new, nil
	  }

	  // or adjust type
	  new, ok := r.(*Example)
	  if !ok {
	    return nil, fmt.Errorf("record not of type *Example, but %T", r)
	  }
	  return new, nil
	}

	func (e *Example) Save() error {
	  return db.Put(e)
	}

	func (e *Example) SaveAs(key string) error {
	  e.SetKey(key)
	  return db.PutNew(e)
	}
*/
package database
