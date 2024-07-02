package database

import (
	"github.com/safing/portmaster/base/database/record"
)

// HookBase implements the Hook interface and provides dummy functions to reduce boilerplate.
type HookBase struct{}

// UsesPreGet implements the Hook interface and returns false.
func (b *HookBase) UsesPreGet() bool {
	return false
}

// UsesPostGet implements the Hook interface and returns false.
func (b *HookBase) UsesPostGet() bool {
	return false
}

// UsesPrePut implements the Hook interface and returns false.
func (b *HookBase) UsesPrePut() bool {
	return false
}

// PreGet implements the Hook interface.
func (b *HookBase) PreGet(dbKey string) error {
	return nil
}

// PostGet implements the Hook interface.
func (b *HookBase) PostGet(r record.Record) (record.Record, error) {
	return r, nil
}

// PrePut implements the Hook interface.
func (b *HookBase) PrePut(r record.Record) (record.Record, error) {
	return r, nil
}
