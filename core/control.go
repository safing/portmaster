package core

import (
	"fmt"
	"strings"
	"sync"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/database/storage"
)

// StorageInterface provices a storage.Interface to the storage manager.
type StorageInterface struct {
	storage.InjectBase
}

// Get returns a database record.
func (s *StorageInterface) Get(key string) (record.Record, error) {
	msg := newMessage(key)
	splittedKey := strings.Split(key, "/")

	switch splittedKey[0] {
	case "module":
		return controlModule(msg, splittedKey)
	default:
		return nil, storage.ErrNotFound
	}
}

func controlModule(msg *MessageRecord, splittedKey []string) (record.Record, error) {
	// format: module/moduleName/action/param
	var moduleName string
	var action string
	var param string
	var err error

	// parse elements
	switch len(splittedKey) {
	case 4:
		param = splittedKey[3]
		fallthrough
	case 3:
		moduleName = splittedKey[1]
		action = splittedKey[2]
	default:
		return nil, storage.ErrNotFound
	}

	// execute
	switch action {
	case "trigger":
		err = module.InjectEvent(fmt.Sprintf("user triggered the '%s/%s' event", moduleName, param), moduleName, param, nil)
	default:
		return nil, storage.ErrNotFound
	}

	if err != nil {
		msg.Message = err.Error()
	} else {
		msg.Success = true
	}

	return msg, nil
}

func registerControlDatabase() error {
	_, err := database.Register(&database.Database{
		Name:        "control",
		Description: "Control Interface for the Portmaster",
		StorageType: "injected",
		PrimaryAPI:  "",
	})
	if err != nil {
		return err
	}

	_, err = database.InjectDatabase("control", &StorageInterface{})
	if err != nil {
		return err
	}

	return nil
}

// MessageRecord is a simple record used for control database feedback
type MessageRecord struct {
	record.Base
	sync.Mutex

	Success bool
	Message string
}

func newMessage(key string) *MessageRecord {
	m := &MessageRecord{}
	m.SetKey("control:" + key)
	m.UpdateMeta()
	return m
}
