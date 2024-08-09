package record

import (
	"errors"

	"github.com/safing/portmaster/base/database/accessor"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

// TODO(ppacher):
//		we can reduce the record.Record interface a lot by moving
//		most of those functions that require the Record as it's first
//		parameter to static package functions
//		(i.e. Marshal, MarshalRecord, GetAccessor, ...).
//		We should also consider given Base a GetBase() *Base method
//		that returns itself. This way we can remove almost all Base
//		only methods from the record.Record interface. That is, we can
//		remove all those CreateMeta, UpdateMeta, ... stuff from the
//		interface definition (not the actual functions!). This would make
// 		the record.Record interface slim and only provide methods that
//		most users actually need. All those database/storage related methods
// 		can still be accessed by using GetBase().XXX() instead. We can also
//		expose the dbName and dbKey and meta properties directly which would
// 		make a nice JSON blob when marshalled.

// Base provides a quick way to comply with the Model interface.
type Base struct {
	dbName string
	dbKey  string
	meta   *Meta
}

// SetKey sets the key on the database record. The key may only be set once and
// future calls to SetKey will be ignored. If you want to copy/move the record
// to another database key, you will need to create a copy and assign a new key.
// A key must be set before the record is used in any database operation.
func (b *Base) SetKey(key string) {
	if !b.KeyIsSet() {
		b.dbName, b.dbKey = ParseKey(key)
	} else {
		log.Errorf("database: key is already set: tried to replace %q with %q", b.Key(), key)
	}
}

// ResetKey resets the database name and key.
// Use with caution!
func (b *Base) ResetKey() {
	b.dbName = ""
	b.dbKey = ""
}

// Key returns the key of the database record.
// As the key must be set before any usage and can only be set once, this
// function may be used without locking the record.
func (b *Base) Key() string {
	return b.dbName + ":" + b.dbKey
}

// KeyIsSet returns true if the database key is set.
// As the key must be set before any usage and can only be set once, this
// function may be used without locking the record.
func (b *Base) KeyIsSet() bool {
	return b.dbName != ""
}

// DatabaseName returns the name of the database.
// As the key must be set before any usage and can only be set once, this
// function may be used without locking the record.
func (b *Base) DatabaseName() string {
	return b.dbName
}

// DatabaseKey returns the database key of the database record.
// As the key must be set before any usage and can only be set once, this
// function may be used without locking the record.
func (b *Base) DatabaseKey() string {
	return b.dbKey
}

// Meta returns the metadata object for this record.
func (b *Base) Meta() *Meta {
	return b.meta
}

// CreateMeta sets a default metadata object for this record.
func (b *Base) CreateMeta() {
	b.meta = &Meta{}
}

// UpdateMeta creates the metadata if it does not exist and updates it.
func (b *Base) UpdateMeta() {
	if b.meta == nil {
		b.CreateMeta()
	}
	b.meta.Update()
}

// SetMeta sets the metadata on the database record, it should only be called after loading the record. Use MoveTo to save the record with another key.
func (b *Base) SetMeta(meta *Meta) {
	b.meta = meta
}

// Marshal marshals the object, without the database key or metadata. It returns nil if the record is deleted.
func (b *Base) Marshal(self Record, format uint8) ([]byte, error) {
	if b.Meta() == nil {
		return nil, errors.New("missing meta")
	}

	if b.Meta().Deleted > 0 {
		return nil, nil
	}

	dumped, err := dsd.Dump(self, format)
	if err != nil {
		return nil, err
	}
	return dumped, nil
}

// MarshalRecord packs the object, including metadata, into a byte array for saving in a database.
func (b *Base) MarshalRecord(self Record) ([]byte, error) {
	if b.Meta() == nil {
		return nil, errors.New("missing meta")
	}

	// version
	c := container.New([]byte{1})

	// meta encoding
	metaSection, err := dsd.Dump(b.meta, dsd.GenCode)
	if err != nil {
		return nil, err
	}
	c.AppendAsBlock(metaSection)

	// data
	dataSection, err := b.Marshal(self, dsd.JSON)
	if err != nil {
		return nil, err
	}
	c.Append(dataSection)

	return c.CompileData(), nil
}

// IsWrapped returns whether the record is a Wrapper.
func (b *Base) IsWrapped() bool {
	return false
}

// GetAccessor returns an accessor for this record, if available.
func (b *Base) GetAccessor(self Record) accessor.Accessor {
	return accessor.NewStructAccessor(self)
}
