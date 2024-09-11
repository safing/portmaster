package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/db/accessor"
	"github.com/safing/structures/dsd"
)

type Record interface {
	Key() string
	Created() time.Time
	Updated() time.Time
	IsDeleted() bool
	Permission() int8

	Format() uint8
	Data() []byte
	Object() any
	Unwrap(target any) error
	GetAccessor() accessor.Accessor
}

type CreatedRecord struct {
	key        string
	created    time.Time
	permission int8

	object any
	format uint8
	data   []byte
}

func NewRecord(key string, permission int8, format uint8, object any) (*CreatedRecord, error) {
	data, err := dsd.Dump(object, format)
	if err != nil {
		return nil, fmt.Errorf("serialize object: %w", err)
	}

	return &CreatedRecord{
		key:        key,
		created:    time.Now(),
		permission: permission,
		format:     format,
		data:       data,
		object:     object,
	}, nil
}

func NewRawRecord(key string, permission int8, format uint8, data []byte) (*CreatedRecord, error) {
	return &CreatedRecord{
		key:        key,
		created:    time.Now(),
		permission: permission,
		format:     format,
		data:       data,
	}, nil
}

func (r *CreatedRecord) Key() string {
	return r.key
}

func (r *CreatedRecord) Created() time.Time {
	return r.created
}

func (r *CreatedRecord) Updated() time.Time {
	return r.created
}

func (r *CreatedRecord) IsDeleted() bool {
	return false
}

func (r *CreatedRecord) Permission() int8 {
	return r.permission
}

func (r *CreatedRecord) Format() uint8 {
	return r.format
}

func (r *CreatedRecord) Data() []byte {
	return r.data
}

func (r *CreatedRecord) Object() any {
	return r.object
}

func (r *CreatedRecord) Unwrap(target any) error {
	return errors.New("new record does not support unwrapping")
}

func (r *CreatedRecord) GetAccessor() accessor.Accessor {
	if r.format == dsd.JSON && len(r.data) > 0 {
		return accessor.NewJSONBytesAccessor(&r.data)
	}
	return nil
}

type DeletedRecord struct {
	key        string
	permission int8
}

func MakeDeletedRecord(r Record) *DeletedRecord {
	return &DeletedRecord{
		key:        r.Key(),
		permission: r.Permission(),
	}
}

func (r *DeletedRecord) Key() string {
	return r.key
}

func (r *DeletedRecord) Created() time.Time {
	return time.Time{}
}

func (r *DeletedRecord) Updated() time.Time {
	return time.Time{}
}

func (r *DeletedRecord) IsDeleted() bool {
	return true
}

func (r *DeletedRecord) Permission() int8 {
	return r.permission
}

func (r *DeletedRecord) Format() uint8 {
	return dsd.AUTO
}

func (r *DeletedRecord) Data() []byte {
	return nil
}

func (r *DeletedRecord) Object() any {
	return nil
}

func (r *DeletedRecord) Unwrap(target any) error {
	return errors.New("deleted record cannot be unwrapped")
}

func (r *DeletedRecord) GetAccessor() accessor.Accessor {
	return nil
}
