package hub

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

var (
	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	getFromNavigator func(mapName, hubID string) *Hub
)

// MakeHubDBKey makes a hub db key.
func MakeHubDBKey(mapName, hubID string) string {
	return fmt.Sprintf("cache:spn/hubs/%s/%s", mapName, hubID)
}

// MakeHubMsgDBKey makes a hub msg db key.
func MakeHubMsgDBKey(mapName string, msgType MsgType, hubID string) string {
	return fmt.Sprintf("cache:spn/msgs/%s/%s/%s", mapName, msgType, hubID)
}

// SetNavigatorAccess sets a shortcut function to access hubs from the navigator instead of having go through the database.
// This also reduces the number of object in RAM and better caches parsed attributes.
func SetNavigatorAccess(fn func(mapName, hubID string) *Hub) {
	if getFromNavigator == nil {
		getFromNavigator = fn
	}
}

// GetHub get a Hub from the database - or the navigator, if configured.
func GetHub(mapName string, hubID string) (*Hub, error) {
	if getFromNavigator != nil {
		hub := getFromNavigator(mapName, hubID)
		if hub != nil {
			return hub, nil
		}
	}

	return GetHubByKey(MakeHubDBKey(mapName, hubID))
}

// GetHubByKey returns a hub by its raw DB key.
func GetHubByKey(key string) (*Hub, error) {
	r, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	hub, err := EnsureHub(r)
	if err != nil {
		return nil, err
	}

	return hub, nil
}

// EnsureHub makes sure a database record is a Hub.
func EnsureHub(r record.Record) (*Hub, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newHub := &Hub{}
		err := record.Unwrap(r, newHub)
		if err != nil {
			return nil, err
		}
		newHub = prepHub(newHub)

		// Fully validate when getting from database.
		if err := newHub.Info.validateFormatting(); err != nil {
			return nil, fmt.Errorf("announcement failed format validation: %w", err)
		}
		if err := newHub.Status.validateFormatting(); err != nil {
			return nil, fmt.Errorf("status failed format validation: %w", err)
		}
		if err := newHub.Info.prepare(false); err != nil {
			return nil, fmt.Errorf("failed to prepare announcement: %w", err)
		}

		return newHub, nil
	}

	// or adjust type
	newHub, ok := r.(*Hub)
	if !ok {
		return nil, fmt.Errorf("record not of type *Hub, but %T", r)
	}
	newHub = prepHub(newHub)

	// Prepare only when already parsed.
	if err := newHub.Info.prepare(false); err != nil {
		return nil, fmt.Errorf("failed to prepare announcement: %w", err)
	}

	// ensure status
	return newHub, nil
}

func prepHub(h *Hub) *Hub {
	if h.Status == nil {
		h.Status = &Status{}
	}
	h.Measurements = getSharedMeasurements(h.ID, h.Measurements)
	return h
}

// Save saves to Hub to the correct scope in the database.
func (h *Hub) Save() error {
	if !h.KeyIsSet() {
		h.SetKey(MakeHubDBKey(h.Map, h.ID))
	}

	return db.Put(h)
}

// RemoveHubAndMsgs deletes a Hub and it's saved messages from the database.
func RemoveHubAndMsgs(mapName string, hubID string) (err error) {
	err = db.Delete(MakeHubDBKey(mapName, hubID))
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("failed to delete main hub entry: %w", err)
	}

	err = db.Delete(MakeHubMsgDBKey(mapName, MsgTypeAnnouncement, hubID))
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("failed to delete hub announcement data: %w", err)
	}

	err = db.Delete(MakeHubMsgDBKey(mapName, MsgTypeStatus, hubID))
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("failed to delete hub status data: %w", err)
	}

	return nil
}

// HubMsg stores raw Hub messages.
type HubMsg struct { //nolint:golint
	record.Base
	sync.Mutex

	ID   string
	Map  string
	Type MsgType
	Data []byte

	Received int64
}

// SaveHubMsg saves a raw (and signed) message received by another Hub.
func SaveHubMsg(id string, mapName string, msgType MsgType, data []byte) error {
	// create wrapper record
	msg := &HubMsg{
		ID:       id,
		Map:      mapName,
		Type:     msgType,
		Data:     data,
		Received: time.Now().Unix(),
	}
	// set key
	msg.SetKey(MakeHubMsgDBKey(msg.Map, msg.Type, msg.ID))
	// save
	return db.PutNew(msg)
}

// QueryRawGossipMsgs queries the database for raw gossip messages.
func QueryRawGossipMsgs(mapName string, msgType MsgType) (it *iterator.Iterator, err error) {
	it, err = db.Query(query.New(MakeHubMsgDBKey(mapName, msgType, "")))
	return
}

// EnsureHubMsg makes sure a database record is a HubMsg.
func EnsureHubMsg(r record.Record) (*HubMsg, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newHubMsg := &HubMsg{}
		err := record.Unwrap(r, newHubMsg)
		if err != nil {
			return nil, err
		}
		return newHubMsg, nil
	}

	// or adjust type
	newHubMsg, ok := r.(*HubMsg)
	if !ok {
		return nil, fmt.Errorf("record not of type *Hub, but %T", r)
	}
	return newHubMsg, nil
}
