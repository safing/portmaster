package cabin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safing/jess"
	"github.com/safing/jess/tools"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
)

const (
	// DefaultIDKeyScheme is the default jess tool for creating ID keys.
	DefaultIDKeyScheme = "Ed25519"

	// DefaultIDKeySecurityLevel is the default security level for creating ID keys.
	DefaultIDKeySecurityLevel = 256 // Ed25519 security level is fixed, setting is ignored.
)

// Identity holds the identity of a Hub.
type Identity struct {
	record.Base

	ID     string
	Map    string
	Hub    *hub.Hub
	Signet *jess.Signet

	ExchKeys map[string]*ExchKey

	infoExportCache   []byte
	statusExportCache []byte
}

// Lock locks the Identity through the Hub lock.
func (id *Identity) Lock() {
	id.Hub.Lock()
}

// Unlock unlocks the Identity through the Hub lock.
func (id *Identity) Unlock() {
	id.Hub.Unlock()
}

// ExchKey holds the private information of a HubKey.
type ExchKey struct {
	Created time.Time
	Expires time.Time
	key     *jess.Signet
	tool    *tools.Tool
}

// CreateIdentity creates a new identity.
func CreateIdentity(ctx context.Context, mapName string) (*Identity, error) {
	id := &Identity{
		Map:      mapName,
		ExchKeys: make(map[string]*ExchKey),
	}

	// create signet
	signet, recipient, err := hub.CreateHubSignet(DefaultIDKeyScheme, DefaultIDKeySecurityLevel)
	if err != nil {
		return nil, err
	}
	id.Signet = signet
	id.ID = signet.ID
	id.Hub = &hub.Hub{
		ID:        id.ID,
		Map:       mapName,
		PublicKey: recipient,
	}

	// initial maintenance routine
	_, err = id.MaintainAnnouncement(nil, true)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize announcement: %w", err)
	}
	_, err = id.MaintainStatus([]*hub.Lane{}, new(int), nil, true)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize status: %w", err)
	}

	return id, nil
}

// MaintainAnnouncement maintains the Hub's Announcenemt and returns whether
// there was a change that should be communicated to other Hubs.
// If newInfo is nil, it will be derived from configuration.
func (id *Identity) MaintainAnnouncement(newInfo *hub.Announcement, selfcheck bool) (changed bool, err error) {
	id.Lock()
	defer id.Unlock()

	// Populate new info with data.
	if newInfo == nil {
		newInfo = getPublicHubInfo()
	}
	newInfo.ID = id.Hub.ID
	if id.Hub.Info != nil {
		newInfo.Timestamp = id.Hub.Info.Timestamp
	}
	if !newInfo.Equal(id.Hub.Info) {
		changed = true
	}

	if changed {
		// Update timestamp.
		newInfo.Timestamp = time.Now().Unix()
	}

	if changed || selfcheck {
		// Export new data.
		newInfoData, err := newInfo.Export(id.signingEnvelope())
		if err != nil {
			return false, fmt.Errorf("failed to export: %w", err)
		}

		// Apply the status as all other Hubs would in order to check if it's valid.
		_, _, _, err = hub.ApplyAnnouncement(id.Hub, newInfoData, conf.MainMapName, conf.MainMapScope, true)
		if err != nil {
			return false, fmt.Errorf("failed to apply new announcement: %w", err)
		}
		id.infoExportCache = newInfoData

		// Save message to hub message storage.
		err = hub.SaveHubMsg(id.ID, conf.MainMapName, hub.MsgTypeAnnouncement, newInfoData)
		if err != nil {
			log.Warningf("spn/cabin: failed to save own new/updated announcement of %s: %s", id.ID, err)
		}
	}

	return changed, nil
}

// MaintainStatus maintains the Hub's Status and returns whether there was a change that should be communicated to other Hubs.
func (id *Identity) MaintainStatus(lanes []*hub.Lane, load *int, flags []string, selfcheck bool) (changed bool, err error) {
	id.Lock()
	defer id.Unlock()

	// Create a new status or make a copy of the status for editing.
	var newStatus *hub.Status
	if id.Hub.Status != nil {
		newStatus = id.Hub.Status.Copy()
	} else {
		newStatus = &hub.Status{}
	}

	// Update software version.
	if newStatus.Version != info.Version() {
		newStatus.Version = info.Version()
		changed = true
	}

	// Update keys.
	keysChanged, err := id.MaintainExchKeys(newStatus, time.Now())
	if err != nil {
		return false, fmt.Errorf("failed to maintain keys: %w", err)
	}
	if keysChanged {
		changed = true
	}

	// Update lanes.
	if lanes != nil && !hub.LanesEqual(newStatus.Lanes, lanes) {
		newStatus.Lanes = lanes
		changed = true
	}

	// Update load.
	if load != nil && newStatus.Load != *load {
		newStatus.Load = *load
		changed = true
	}

	// Update flags.
	if !hub.FlagsEqual(newStatus.Flags, flags) {
		newStatus.Flags = flags
		changed = true
	}

	// Update timestamp if something changed.
	if changed {
		newStatus.Timestamp = time.Now().Unix()
	}

	if changed || selfcheck {
		// Export new data.
		newStatusData, err := newStatus.Export(id.signingEnvelope())
		if err != nil {
			return false, fmt.Errorf("failed to export: %w", err)
		}

		// Apply the status as all other Hubs would in order to check if it's valid.
		_, _, _, err = hub.ApplyStatus(id.Hub, newStatusData, conf.MainMapName, conf.MainMapScope, true)
		if err != nil {
			return false, fmt.Errorf("failed to apply new status: %w", err)
		}
		id.statusExportCache = newStatusData

		// Save message to hub message storage.
		err = hub.SaveHubMsg(id.ID, conf.MainMapName, hub.MsgTypeStatus, newStatusData)
		if err != nil {
			log.Warningf("spn/cabin: failed to save own new/updated status: %s", err)
		}
	}

	return changed, nil
}

// MakeOfflineStatus creates and signs an offline status message.
func (id *Identity) MakeOfflineStatus() (offlineStatusExport []byte, err error) {
	// Make offline status.
	newStatus := &hub.Status{
		Timestamp: time.Now().Unix(),
		Version:   info.Version(),
		Flags:     []string{hub.FlagOffline},
	}

	// Export new data.
	newStatusData, err := newStatus.Export(id.signingEnvelope())
	if err != nil {
		return nil, fmt.Errorf("failed to export: %w", err)
	}

	return newStatusData, nil
}

func (id *Identity) signingEnvelope() *jess.Envelope {
	env := jess.NewUnconfiguredEnvelope()
	env.SuiteID = jess.SuiteSignV1
	env.Senders = []*jess.Signet{id.Signet}

	return env
}

// ExportAnnouncement serializes and signs the Announcement.
func (id *Identity) ExportAnnouncement() ([]byte, error) {
	id.Lock()
	defer id.Unlock()

	if id.infoExportCache == nil {
		return nil, errors.New("announcement not exported")
	}

	return id.infoExportCache, nil
}

// ExportStatus serializes and signs the Status.
func (id *Identity) ExportStatus() ([]byte, error) {
	id.Lock()
	defer id.Unlock()

	if id.statusExportCache == nil {
		return nil, errors.New("status not exported")
	}

	return id.statusExportCache, nil
}

// SignHubMsg signs a data blob with the identity's private key.
func (id *Identity) SignHubMsg(data []byte) ([]byte, error) {
	return hub.SignHubMsg(data, id.signingEnvelope(), false)
}

// GetSignet returns the private exchange key with the given ID.
func (id *Identity) GetSignet(keyID string, recipient bool) (*jess.Signet, error) {
	if recipient {
		return nil, errors.New("cabin.Identity only serves private keys")
	}

	id.Lock()
	defer id.Unlock()

	key, ok := id.ExchKeys[keyID]
	if !ok {
		return nil, errors.New("the requested key does not exist")
	}
	if time.Now().After(key.Expires) || key.key == nil {
		return nil, errors.New("the requested key has expired")
	}

	return key.key, nil
}

func (ek *ExchKey) toHubKey() (*hub.Key, error) {
	if ek.key == nil {
		return nil, errors.New("no key")
	}

	// export public key
	rcpt, err := ek.key.AsRecipient()
	if err != nil {
		return nil, err
	}
	err = rcpt.StoreKey()
	if err != nil {
		return nil, err
	}

	// repackage
	return &hub.Key{
		Scheme:  rcpt.Scheme,
		Key:     rcpt.Key,
		Expires: ek.Expires.Unix(),
	}, nil
}
