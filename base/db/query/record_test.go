package query

import (
	"errors"
	"fmt"

	"github.com/safing/portmaster/base/db/accessor"
	"github.com/safing/structures/dsd"
)

type TestRecord struct {
	key string

	object any
	format uint8
	data   []byte
}

func NewRecord(key string, format uint8, object any) (*TestRecord, error) {
	data, err := dsd.Dump(object, format)
	if err != nil {
		return nil, fmt.Errorf("serialize object: %w", err)
	}

	return &TestRecord{
		key:    key,
		format: format,
		data:   data,
		object: object,
	}, nil
}

func NewRawRecord(key string, format uint8, data []byte) (*TestRecord, error) {
	return &TestRecord{
		key:    key,
		format: format,
		data:   data,
	}, nil
}

func (r *TestRecord) Key() string {
	return r.key
}

func (r *TestRecord) Permission() int8 {
	return 0
}

func (r *TestRecord) Format() uint8 {
	return r.format
}

func (r *TestRecord) Data() []byte {
	return r.data
}

func (r *TestRecord) Object() any {
	return r.object
}

func (r *TestRecord) Unwrap(target any) error {
	return errors.New("new record does not support unwrapping")
}

func (r *TestRecord) GetAccessor() accessor.Accessor {
	if r.format == dsd.JSON && len(r.data) > 0 {
		return accessor.NewJSONBytesAccessor(&r.data)
	}
	return nil
}
