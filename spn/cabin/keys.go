package cabin

import (
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/safing/jess"
	"github.com/safing/jess/tools"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/spn/hub"
)

type providedExchKeyScheme struct {
	id            string
	securityLevel int //nolint:structcheck // TODO
	tool          *tools.Tool
}

var (
	// validFor defines how long keys are valid for use by clients.
	validFor = 48 * time.Hour // 2 days
	// renewBeforeExpiry defines the duration how long before expiry keys should be renewed.
	renewBeforeExpiry = 24 * time.Hour // 1 day

	// burnAfter defines how long after expiry keys are burnt/deleted.
	burnAfter = 12 * time.Hour // 1/2 day
	// reuseAfter defines how long IDs should be blocked after expiry (and not be reused for new keys).
	reuseAfter = 2 * 7 * 24 * time.Hour // 2 weeks

	// provideExchKeySchemes defines the jess tools for creating exchange keys.
	provideExchKeySchemes = []*providedExchKeyScheme{
		{
			id:            "ECDH-X25519",
			securityLevel: 128, // informative only, security level of ECDH-X25519 is fixed
		},
		// TODO: test with rsa keys
	}
)

func initProvidedExchKeySchemes() error {
	for _, eks := range provideExchKeySchemes {
		tool, err := tools.Get(eks.id)
		if err != nil {
			return err
		}
		eks.tool = tool
	}
	return nil
}

// MaintainExchKeys maintains the exchange keys, creating new ones and
// deprecating and deleting old ones.
func (id *Identity) MaintainExchKeys(newStatus *hub.Status, now time.Time) (changed bool, err error) {
	// create Keys map
	if id.ExchKeys == nil {
		id.ExchKeys = make(map[string]*ExchKey)
	}

	// lifecycle management
	for keyID, exchKey := range id.ExchKeys {
		if exchKey.key != nil && now.After(exchKey.Expires.Add(burnAfter)) {
			// delete key
			err := exchKey.tool.StaticLogic.BurnKey(exchKey.key)
			if err != nil {
				log.Warningf(
					"spn/cabin: failed to burn key %s (%s) of %s: %s",
					keyID,
					exchKey.tool.Info.Name,
					id.Hub.ID,
					err,
				)
			}
			// remove reference
			exchKey.key = nil
		}
		if now.After(exchKey.Expires.Add(reuseAfter)) {
			// remove key
			delete(id.ExchKeys, keyID)
		}
	}

	// find or create current keys
	for _, eks := range provideExchKeySchemes {
		found := false
		for _, exchKey := range id.ExchKeys {
			if exchKey.key != nil &&
				exchKey.key.Scheme == eks.id &&
				now.Before(exchKey.Expires.Add(-renewBeforeExpiry)) {
				found = true
				break
			}
		}

		if !found {
			err := id.createExchKey(eks, now)
			if err != nil {
				return false, fmt.Errorf("failed to create %s exchange key: %w", eks.tool.Info.Name, err)
			}
			changed = true
		}
	}

	// export most recent keys to HubStatus
	if changed || len(newStatus.Keys) == 0 {
		// reset
		newStatus.Keys = make(map[string]*hub.Key)

		// find longest valid key for every provided scheme
		for _, eks := range provideExchKeySchemes {
			// find key of scheme that is valid the longest
			longestValid := &ExchKey{
				Expires: now,
			}
			for _, exchKey := range id.ExchKeys {
				if exchKey.key != nil &&
					exchKey.key.Scheme == eks.id &&
					exchKey.Expires.After(longestValid.Expires) {
					longestValid = exchKey
				}
			}

			// check result
			if longestValid.key == nil {
				log.Warningf("spn/cabin: could not find export candidate for exchange key scheme %s", eks.id)
				continue
			}

			// export
			hubKey, err := longestValid.toHubKey()
			if err != nil {
				return false, fmt.Errorf("failed to export %s exchange key: %w", longestValid.tool.Info.Name, err)
			}
			// add
			newStatus.Keys[longestValid.key.ID] = hubKey
		}
	}

	return changed, nil
}

func (id *Identity) createExchKey(eks *providedExchKeyScheme, now time.Time) error {
	// get ID
	var keyID string
	for range 1000000 { // not forever
		// generate new ID
		b, err := rng.Bytes(3)
		if err != nil {
			return fmt.Errorf("failed to get random data for key ID: %w", err)
		}
		keyID = base64.RawURLEncoding.EncodeToString(b)
		_, exists := id.ExchKeys[keyID]
		if !exists {
			break
		}
	}
	if keyID == "" {
		return errors.New("unable to find available exchange key ID")
	}

	// generate key
	signet := jess.NewSignetBase(eks.tool)
	signet.ID = keyID
	// TODO: use security level for key generation
	if err := signet.GenerateKey(); err != nil {
		return fmt.Errorf("failed to get new exchange key: %w", err)
	}

	// add to key map
	id.ExchKeys[keyID] = &ExchKey{
		Created: now,
		Expires: now.Add(validFor),
		key:     signet,
		tool:    eks.tool,
	}
	return nil
}
