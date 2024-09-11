package hashmap

import (
	"time"

	"github.com/safing/portmaster/base/db/accessor"
	"github.com/safing/structures/dsd"
)

type HashMapRecord struct {
	key        string
	created    time.Time
	updated    time.Time
	permission int8

	object any
	format uint8
	data   []byte
}

func (r *HashMapRecord) Key() string {
	return r.key
}

func (r *HashMapRecord) Created() time.Time {
	return r.created
}

func (r *HashMapRecord) Updated() time.Time {
	return r.updated
}

func (r *HashMapRecord) IsDeleted() bool {
	return false
}

func (r *HashMapRecord) Permission() int8 {
	return r.permission
}

func (r *HashMapRecord) Format() uint8 {
	return r.format
}

func (r *HashMapRecord) Data() []byte {
	return r.data
}

func (r *HashMapRecord) Object() any {
	return r.object
}

func (r *HashMapRecord) Unwrap(target any) error {
	err := dsd.LoadAsFormat(r.data, r.format, target)
	if err != nil {
		return err
	}

	r.object = target
	return nil
}

func (r *HashMapRecord) GetAccessor() accessor.Accessor {
	if r.format == dsd.JSON && len(r.data) > 0 {
		return accessor.NewJSONBytesAccessor(&r.data)
	}
	return nil
}

func (r *HashMapRecord) Copy() *HashMapRecord {
	return &HashMapRecord{
		key:        r.key,
		created:    r.created,
		updated:    r.updated,
		permission: r.permission,
		object:     r.object,
		format:     r.format,
		data:       r.data,
	}
}
