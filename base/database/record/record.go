package record

import (
	"github.com/safing/portmaster/base/database/accessor"
)

// Record provides an interface for uniformally handling database records.
type Record interface {
	SetKey(key string) // test:config
	Key() string       // test:config
	KeyIsSet() bool
	DatabaseName() string // test
	DatabaseKey() string  // config

	// Metadata.
	Meta() *Meta
	SetMeta(meta *Meta)
	CreateMeta()
	UpdateMeta()

	// Serialization.
	Marshal(self Record, format uint8) ([]byte, error)
	MarshalRecord(self Record) ([]byte, error)
	GetAccessor(self Record) accessor.Accessor

	// Locking.
	Lock()
	Unlock()

	// Wrapping.
	IsWrapped() bool
}
