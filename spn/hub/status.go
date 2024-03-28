package hub

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"golang.org/x/exp/slices"

	"github.com/safing/jess"
)

// VersionOffline is a special version used to signify that the Hub has gone offline.
// This is depracated, please use FlagOffline instead.
const VersionOffline = "offline"

// Status Flags.
const (
	// FlagNetError signifies that the Hub reports a network connectivity failure or impairment.
	FlagNetError = "net-error"

	// FlagOffline signifies that the Hub has gone offline by itself.
	FlagOffline = "offline"

	// FlagAllowUnencrypted signifies that the Hub is available to handle unencrypted connections.
	FlagAllowUnencrypted = "allow-unencrypted"
)

// Status is the message type used to update changing Hub Information. Changes are made automatically.
type Status struct {
	Timestamp int64 `cbor:"t"`

	// Version holds the current software version of the Hub.
	Version string `cbor:"v"`

	// Routing Information
	Keys  map[string]*Key `cbor:"k,omitempty" json:",omitempty"` // public keys (with type)
	Lanes []*Lane         `cbor:"c,omitempty" json:",omitempty"` // Connections to other Hubs.

	// Status Information
	// Load describes max(CPU, Memory) in percent, averaged over at least 15
	// minutes. Load is published in fixed steps only.
	Load int `cbor:"l,omitempty" json:",omitempty"`

	// Flags holds flags that signify special states.
	Flags []string `cbor:"f,omitempty" json:",omitempty"`
}

// Key represents a semi-ephemeral public key used for 0-RTT connection establishment.
type Key struct {
	Scheme  string
	Key     []byte
	Expires int64
}

// Lane represents a connection to another Hub.
type Lane struct {
	// ID is the Hub ID of the peer.
	ID string

	// Capacity designates the available bandwidth between these Hubs.
	// It is specified in bit/s.
	Capacity int

	// Lateny designates the latency between these Hubs.
	// It is specified in nanoseconds.
	Latency time.Duration
}

// Copy returns a deep copy of the Status.
func (s *Status) Copy() *Status {
	newStatus := &Status{
		Timestamp: s.Timestamp,
		Version:   s.Version,
		Lanes:     slices.Clone(s.Lanes),
		Load:      s.Load,
		Flags:     slices.Clone(s.Flags),
	}
	// Copy map.
	newStatus.Keys = make(map[string]*Key, len(s.Keys))
	for k, v := range s.Keys {
		newStatus.Keys[k] = v
	}
	return newStatus
}

// SelectSignet selects the public key to use for initiating connections to that Hub.
func (h *Hub) SelectSignet() *jess.Signet {
	h.Lock()
	defer h.Unlock()

	// Return no Signet if we don't have a Status.
	if h.Status == nil {
		return nil
	}

	// TODO: select key based on preferred alg?
	now := time.Now().Unix()
	for id, key := range h.Status.Keys {
		if now < key.Expires {
			return &jess.Signet{
				ID:     id,
				Scheme: key.Scheme,
				Key:    key.Key,
				Public: true,
			}
		}
	}

	return nil
}

// GetSignet returns the public key identified by the given ID from the Hub Status.
func (h *Hub) GetSignet(id string, recipient bool) (*jess.Signet, error) {
	h.Lock()
	defer h.Unlock()

	// check if public key is being requested
	if !recipient {
		return nil, jess.ErrSignetNotFound
	}
	// check if ID exists
	key, ok := h.Status.Keys[id]
	if !ok {
		return nil, jess.ErrSignetNotFound
	}
	// transform and return
	return &jess.Signet{
		ID:     id,
		Scheme: key.Scheme,
		Key:    key.Key,
		Public: true,
	}, nil
}

// AddLane adds a new Lane to the Hub Status.
func (h *Hub) AddLane(newLane *Lane) error {
	h.Lock()
	defer h.Unlock()

	// validity check
	if h.Status == nil {
		return ErrMissingInfo
	}

	// check if duplicate
	for _, lane := range h.Status.Lanes {
		if newLane.ID == lane.ID {
			return errors.New("lane already exists")
		}
	}

	// add
	h.Status.Lanes = append(h.Status.Lanes, newLane)
	return nil
}

// RemoveLane removes a Lane from the Hub Status.
func (h *Hub) RemoveLane(hubID string) error {
	h.Lock()
	defer h.Unlock()

	// validity check
	if h.Status == nil {
		return ErrMissingInfo
	}

	for key, lane := range h.Status.Lanes {
		if lane.ID == hubID {
			h.Status.Lanes = append(h.Status.Lanes[:key], h.Status.Lanes[key+1:]...)
			break
		}
	}

	return nil
}

// GetLaneTo returns the lane to the given Hub, if it exists.
func (h *Hub) GetLaneTo(hubID string) *Lane {
	h.Lock()
	defer h.Unlock()

	// validity check
	if h.Status == nil {
		return nil
	}

	for _, lane := range h.Status.Lanes {
		if lane.ID == hubID {
			return lane
		}
	}

	return nil
}

// Equal returns whether the Lane is equal to the given one.
func (l *Lane) Equal(other *Lane) bool {
	switch {
	case l == nil || other == nil:
		return false
	case l.ID != other.ID:
		return false
	case l.Capacity != other.Capacity:
		return false
	case l.Latency != other.Latency:
		return false
	}
	return true
}

// validateFormatting check if all values conform to the basic format.
func (s *Status) validateFormatting() error {
	// public keys
	if len(s.Keys) > 255 {
		return fmt.Errorf("field Keys with array/slice length of %d exceeds max length of %d", len(s.Keys), 255)
	}
	for keyID, key := range s.Keys {
		if err := checkStringFormat("Keys#ID", keyID, 255); err != nil {
			return err
		}
		if err := checkStringFormat("Keys.Scheme", key.Scheme, 255); err != nil {
			return err
		}
		if err := checkByteSliceFormat("Keys.Key", key.Key, 1024); err != nil {
			return err
		}
	}

	// connections
	if len(s.Lanes) > 255 {
		return fmt.Errorf("field Lanes with array/slice length of %d exceeds max length of %d", len(s.Lanes), 255)
	}
	for _, lanes := range s.Lanes {
		if err := checkStringFormat("Lanes.ID", lanes.ID, 255); err != nil {
			return err
		}
	}

	// Flags
	if err := checkStringSliceFormat("Flags", s.Flags, 255, 255); err != nil {
		return err
	}

	return nil
}

func (l *Lane) String() string {
	return fmt.Sprintf("<%s cap=%d lat=%d>", l.ID, l.Capacity, l.Latency)
}

// LanesEqual returns whether the given []*Lane are equal.
func LanesEqual(a, b []*Lane) bool {
	if len(a) != len(b) {
		return false
	}

	for i, l := range a {
		if !l.Equal(b[i]) {
			return false
		}
	}

	return true
}

type lanes []*Lane

func (l lanes) Len() int           { return len(l) }
func (l lanes) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l lanes) Less(i, j int) bool { return l[i].ID < l[j].ID }

// SortLanes sorts a slice of Lanes.
func SortLanes(l []*Lane) {
	sort.Sort(lanes(l))
}

// HasFlag returns whether the Status has the given flag set.
func (s *Status) HasFlag(flagName string) bool {
	return slices.Contains[[]string, string](s.Flags, flagName)
}

// FlagsEqual returns whether the given status flags are equal.
func FlagsEqual(a, b []string) bool {
	// Cannot be equal if lengths are different.
	if len(a) != len(b) {
		return false
	}

	// If both are empty, they are equal.
	if len(a) == 0 {
		return true
	}

	// Make sure flags are sorted before comparing values.
	sort.Strings(a)
	sort.Strings(b)

	// Compare values.
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}

	return true
}
