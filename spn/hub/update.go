package hub

import (
	"errors"
	"fmt"
	"time"

	"github.com/safing/jess"
	"github.com/safing/jess/lhash"
	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

var (
	// hubMsgRequirements defines which security attributes message need to have.
	hubMsgRequirements = jess.NewRequirements().
				Remove(jess.RecipientAuthentication). // Recipient don't need a private key.
				Remove(jess.Confidentiality).         // Message contents are out in the open.
				Remove(jess.Integrity)                // Only applies to decryption.
	// SenderAuthentication provides pre-decryption integrity. That is all we need.

	clockSkewTolerance = 12 * time.Hour
)

// SignHubMsg signs the given serialized hub msg with the given configuration.
func SignHubMsg(msg []byte, env *jess.Envelope, enableTofu bool) ([]byte, error) {
	// start session from envelope
	session, err := env.Correspondence(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate signing session: %w", err)
	}
	// sign the data
	letter, err := session.Close(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to sign msg: %w", err)
	}

	if enableTofu {
		// smuggle the public key
		// letter.Keys is usually only used for key exchanges and encapsulation
		// neither is used when signing, so we can use letter.Keys to transport public keys
		for _, sender := range env.Senders {
			// get public key
			public, err := sender.AsRecipient()
			if err != nil {
				return nil, fmt.Errorf("failed to get public key of %s: %w", sender.ID, err)
			}
			// serialize key
			err = public.StoreKey()
			if err != nil {
				return nil, fmt.Errorf("failed to serialize public key %s: %w", sender.ID, err)
			}
			// add to keys
			letter.Keys = append(letter.Keys, &jess.Seal{
				Value: public.Key,
			})
		}
	}

	// pack
	data, err := letter.ToDSD(dsd.JSON)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// OpenHubMsg opens a signed hub msg and verifies the signature using the
// provided hub or the local database. If TOFU is enabled, the signature is
// always accepted, if valid.
func OpenHubMsg(hub *Hub, data []byte, mapName string, tofu bool) (msg []byte, sendingHub *Hub, known bool, err error) {
	letter, err := jess.LetterFromDSD(data)
	if err != nil {
		return nil, nil, false, fmt.Errorf("malformed letter: %w", err)
	}

	// check signatures
	var seal *jess.Seal
	switch len(letter.Signatures) {
	case 0:
		return nil, nil, false, errors.New("missing signature")
	case 1:
		seal = letter.Signatures[0]
	default:
		return nil, nil, false, fmt.Errorf("too many signatures (%d)", len(letter.Signatures))
	}

	// check signature signer ID
	if seal.ID == "" {
		return nil, nil, false, errors.New("signature is missing signer ID")
	}

	// get hub for public key
	if hub == nil {
		hub, err = GetHub(mapName, seal.ID)
		if err != nil {
			if !errors.Is(err, database.ErrNotFound) {
				return nil, nil, false, fmt.Errorf("failed to get existing hub %s: %w", seal.ID, err)
			}
			hub = nil
		} else {
			known = true
		}
	} else {
		known = true
	}

	var truststore jess.TrustStore
	if hub != nil && hub.PublicKey != nil { // bootstrap entries will not have a public key
		// check ID integrity
		if hub.ID != seal.ID {
			return nil, hub, known, fmt.Errorf("ID mismatch with hub msg ID %s and hub ID %s", seal.ID, hub.ID)
		}
		if !verifyHubID(seal.ID, hub.PublicKey.Scheme, hub.PublicKey.Key) {
			return nil, hub, known, fmt.Errorf("ID integrity of %s violated with existing key", seal.ID)
		}
	} else {
		if !tofu {
			return nil, nil, false, fmt.Errorf("hub msg ID %s unknown (missing announcement)", seal.ID)
		}

		// trust on first use, extract key from keys
		// TODO: Test if works without TOFU.

		// get key
		var pubkey *jess.Seal
		switch len(letter.Keys) {
		case 0:
			return nil, nil, false, fmt.Errorf("missing key for TOFU of %s", seal.ID)
		case 1:
			pubkey = letter.Keys[0]
		default:
			return nil, nil, false, fmt.Errorf("too many keys (%d) for TOFU of %s", len(letter.Keys), seal.ID)
		}

		// check ID integrity
		if !verifyHubID(seal.ID, seal.Scheme, pubkey.Value) {
			return nil, nil, false, fmt.Errorf("ID integrity of %s violated with new key", seal.ID)
		}

		hub = &Hub{
			ID:  seal.ID,
			Map: mapName,
			PublicKey: &jess.Signet{
				ID:     seal.ID,
				Scheme: seal.Scheme,
				Key:    pubkey.Value,
				Public: true,
			},
		}
		err = hub.PublicKey.LoadKey()
		if err != nil {
			return nil, nil, false, err
		}
	}

	// create trust store
	truststore = &SingleTrustStore{hub.PublicKey}

	// remove keys from letter, as they are only used to transfer the public key
	letter.Keys = nil

	// check signature
	err = letter.Verify(hubMsgRequirements, truststore)
	if err != nil {
		return nil, nil, false, err
	}

	return letter.Data, hub, known, nil
}

// Export exports the announcement with the given signature configuration.
func (a *Announcement) Export(env *jess.Envelope) ([]byte, error) {
	// pack
	msg, err := dsd.Dump(a, dsd.JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to pack announcement: %w", err)
	}

	return SignHubMsg(msg, env, true)
}

// ApplyAnnouncement applies the announcement to the Hub if it passes all the
// checks. If no Hub is provided, it is loaded from the database or created.
func ApplyAnnouncement(existingHub *Hub, data []byte, mapName string, scope Scope, selfcheck bool) (hub *Hub, known, changed bool, err error) {
	// Set valid/invalid status based on the return error.
	var announcement *Announcement
	defer func() {
		if hub != nil {
			if err != nil && !errors.Is(err, ErrOldData) {
				hub.InvalidInfo = true
			} else {
				hub.InvalidInfo = false
			}
		}
	}()

	// open and verify
	var msg []byte
	msg, hub, known, err = OpenHubMsg(existingHub, data, mapName, true)

	// Lock hub if we have one.
	if hub != nil && !selfcheck {
		hub.Lock()
		defer hub.Unlock()
	}

	// Check if there was an error with the Hub msg.
	if err != nil {
		return //nolint:nakedret
	}

	// parse
	announcement = &Announcement{}
	_, err = dsd.Load(msg, announcement)
	if err != nil {
		return //nolint:nakedret
	}

	// integrity check

	// `hub.ID` is taken from the first ever received announcement message.
	// `announcement.ID` is additionally present in the message as we need
	// a signed version of the ID to mitigate fake IDs.
	// Fake IDs are possible because the hash algorithm of the ID is dynamic.
	if hub.ID != announcement.ID {
		err = fmt.Errorf("announcement ID %q mismatches hub ID %q", announcement.ID, hub.ID)
		return //nolint:nakedret
	}

	// version check
	if hub.Info != nil {
		// check if we already have this version
		switch {
		case announcement.Timestamp == hub.Info.Timestamp && !selfcheck:
			// The new copy is not saved, as we expect the versions to be identical.
			// Also, the new version has not been validated at this point.
			return //nolint:nakedret
		case announcement.Timestamp < hub.Info.Timestamp:
			// Received an old version, do not update.
			err = fmt.Errorf(
				"%wannouncement from %s @ %s is older than current status @ %s",
				ErrOldData, hub.StringWithoutLocking(), time.Unix(announcement.Timestamp, 0), time.Unix(hub.Info.Timestamp, 0),
			)
			return //nolint:nakedret
		}
	}

	// We received a new version.
	changed = true

	// Update timestamp here already in case validation fails.
	if hub.Info != nil {
		hub.Info.Timestamp = announcement.Timestamp
	}

	// Validate the announcement.
	err = hub.validateAnnouncement(announcement, scope)
	if err != nil {
		if selfcheck || hub.FirstSeen.IsZero() {
			err = fmt.Errorf("failed to validate announcement of %s: %w", hub.StringWithoutLocking(), err)
			return //nolint:nakedret
		}

		log.Warningf("spn/hub: received an invalid announcement of %s: %s", hub.StringWithoutLocking(), err)
		// If a previously fully validated Hub publishes an update that breaks it, a
		// soft-fail will accept the faulty changes, but mark is as invalid and
		// forward it to neighbors. This way the invalid update is propagated through
		// the network and all nodes will mark it as invalid an thus ingore the Hub
		// until the issue is fixed.
	}

	// Only save announcement if it is valid.
	if err == nil {
		hub.Info = announcement
	}
	// Set FirstSeen timestamp when we see this Hub for the first time.
	if hub.FirstSeen.IsZero() {
		hub.FirstSeen = time.Now().UTC()
	}

	return //nolint:nakedret
}

func (h *Hub) validateAnnouncement(announcement *Announcement, scope Scope) error {
	// value formatting
	if err := announcement.validateFormatting(); err != nil {
		return err
	}
	// check parsables
	if err := announcement.prepare(true); err != nil {
		return fmt.Errorf("failed to prepare announcement: %w", err)
	}

	// check timestamp
	if announcement.Timestamp > time.Now().Add(clockSkewTolerance).Unix() {
		return fmt.Errorf(
			"announcement from %s @ %s is from the future",
			announcement.ID,
			time.Unix(announcement.Timestamp, 0),
		)
	}

	// check for illegal IP address changes
	if h.Info != nil {
		switch {
		case h.Info.IPv4 != nil && announcement.IPv4 == nil:
			h.VerifiedIPs = false
			return errors.New("previously announced IPv4 address missing")
		case h.Info.IPv4 != nil && !announcement.IPv4.Equal(h.Info.IPv4):
			h.VerifiedIPs = false
			return errors.New("IPv4 address changed")
		case h.Info.IPv6 != nil && announcement.IPv6 == nil:
			h.VerifiedIPs = false
			return errors.New("previously announced IPv6 address missing")
		case h.Info.IPv6 != nil && !announcement.IPv6.Equal(h.Info.IPv6):
			h.VerifiedIPs = false
			return errors.New("IPv6 address changed")
		}
	}

	// validate IP scopes
	if announcement.IPv4 != nil {
		ipScope := netutils.GetIPScope(announcement.IPv4)
		switch {
		case scope == ScopeLocal && !ipScope.IsLAN():
			return errors.New("IPv4 scope violation: outside of local scope")
		case scope == ScopePublic && !ipScope.IsGlobal():
			return errors.New("IPv4 scope violation: outside of global scope")
		}
		// Reset IP verification flag if IPv4 was added.
		if h.Info == nil || h.Info.IPv4 == nil {
			h.VerifiedIPs = false
		}
	}
	if announcement.IPv6 != nil {
		ipScope := netutils.GetIPScope(announcement.IPv6)
		switch {
		case scope == ScopeLocal && !ipScope.IsLAN():
			return errors.New("IPv6 scope violation: outside of local scope")
		case scope == ScopePublic && !ipScope.IsGlobal():
			return errors.New("IPv6 scope violation: outside of global scope")
		}
		// Reset IP verification flag if IPv6 was added.
		if h.Info == nil || h.Info.IPv6 == nil {
			h.VerifiedIPs = false
		}
	}

	return nil
}

// Export exports the status with the given signature configuration.
func (s *Status) Export(env *jess.Envelope) ([]byte, error) {
	// pack
	msg, err := dsd.Dump(s, dsd.JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to pack status: %w", err)
	}

	return SignHubMsg(msg, env, false)
}

// ApplyStatus applies a status update if it passes all the checks.
func ApplyStatus(existingHub *Hub, data []byte, mapName string, scope Scope, selfcheck bool) (hub *Hub, known, changed bool, err error) {
	// Set valid/invalid status based on the return error.
	defer func() {
		if hub != nil {
			if err != nil && !errors.Is(err, ErrOldData) {
				hub.InvalidStatus = true
			} else {
				hub.InvalidStatus = false
			}
		}
	}()

	// open and verify
	var msg []byte
	msg, hub, known, err = OpenHubMsg(existingHub, data, mapName, false)

	// Lock hub if we have one.
	if hub != nil && !selfcheck {
		hub.Lock()
		defer hub.Unlock()
	}

	// Check if there was an error with the Hub msg.
	if err != nil {
		return //nolint:nakedret
	}

	// parse
	status := &Status{}
	_, err = dsd.Load(msg, status)
	if err != nil {
		return //nolint:nakedret
	}

	// version check
	if hub.Status != nil {
		// check if we already have this version
		switch {
		case status.Timestamp == hub.Status.Timestamp && !selfcheck:
			// The new copy is not saved, as we expect the versions to be identical.
			// Also, the new version has not been validated at this point.
			return //nolint:nakedret
		case status.Timestamp < hub.Status.Timestamp:
			// Received an old version, do not update.
			err = fmt.Errorf(
				"%wstatus from %s @ %s is older than current status @ %s",
				ErrOldData, hub.StringWithoutLocking(), time.Unix(status.Timestamp, 0), time.Unix(hub.Status.Timestamp, 0),
			)
			return //nolint:nakedret
		}
	}

	// We received a new version.
	changed = true

	// Update timestamp here already in case validation fails.
	if hub.Status != nil {
		hub.Status.Timestamp = status.Timestamp
	}

	// Validate the status.
	err = hub.validateStatus(status)
	if err != nil {
		if selfcheck {
			err = fmt.Errorf("failed to validate status of %s: %w", hub.StringWithoutLocking(), err)
			return //nolint:nakedret
		}

		log.Warningf("spn/hub: received an invalid status of %s: %s", hub.StringWithoutLocking(), err)
		// If a previously fully validated Hub publishes an update that breaks it, a
		// soft-fail will accept the faulty changes, but mark is as invalid and
		// forward it to neighbors. This way the invalid update is propagated through
		// the network and all nodes will mark it as invalid an thus ingore the Hub
		// until the issue is fixed.
	}

	// Only save status if it is valid, else mark it as invalid.
	if err == nil {
		hub.Status = status
	}

	return //nolint:nakedret
}

func (h *Hub) validateStatus(status *Status) error {
	// value formatting
	if err := status.validateFormatting(); err != nil {
		return err
	}

	// check timestamp
	if status.Timestamp > time.Now().Add(clockSkewTolerance).Unix() {
		return fmt.Errorf(
			"status from %s @ %s is from the future",
			h.ID,
			time.Unix(status.Timestamp, 0),
		)
	}

	// TODO: validate status.Keys

	return nil
}

// CreateHubSignet creates a signet with the correct ID for usage as a Hub Identity.
func CreateHubSignet(toolID string, securityLevel int) (private, public *jess.Signet, err error) {
	private, err = jess.GenerateSignet(toolID, securityLevel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}
	err = private.StoreKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to store private key: %w", err)
	}

	// get public key for creating the Hub ID
	public, err = private.AsRecipient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get public key: %w", err)
	}
	err = public.StoreKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to store public key: %w", err)
	}

	// assign IDs
	private.ID = createHubID(public.Scheme, public.Key)
	public.ID = private.ID

	return private, public, nil
}

func createHubID(scheme string, pubkey []byte) string {
	// compile scheme and public key
	c := container.New()
	c.AppendAsBlock([]byte(scheme))
	c.AppendAsBlock(pubkey)

	return lhash.Digest(lhash.BLAKE2b_256, c.CompileData()).Base58()
}

func verifyHubID(id string, scheme string, pubkey []byte) (ok bool) {
	// load labeled hash from ID
	labeledHash, err := lhash.FromBase58(id)
	if err != nil {
		return false
	}

	// compile scheme and public key
	c := container.New()
	c.AppendAsBlock([]byte(scheme))
	c.AppendAsBlock(pubkey)

	// check if it matches
	return labeledHash.MatchesData(c.CompileData())
}
