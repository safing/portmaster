package netquery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/formats/dsd"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/runtime"
	"github.com/safing/portmaster/network"
)

type (
	// ConnectionStore describes the interface that is used by Manager
	// to save new or updated connection objects.
	// It is implemented by the *Database type of this package.
	ConnectionStore interface {
		// Save is called to perists the new or updated connection. If required,
		// It's up the the implementation to figure out if the operation is an
		// insert or an update.
		// The ID of Conn is unique and can be trusted to never collide with other
		// connections of the save device.
		Save(context.Context, Conn) error
	}

	// Manager handles new and updated network.Connections feeds and persists them
	// at a connection store.
	// Manager also registers itself as a runtime database and pushes updates to
	// connections using the local format.
	// Users should use this update feed rather than the deprecated "network:" database.
	Manager struct {
		store      ConnectionStore
		push       runtime.PushFunc
		runtimeReg *runtime.Registry
		pushPrefix string
	}
)

// NewManager returns a new connection manager that persists all newly created or
// updated connections at store.
func NewManager(store ConnectionStore, pushPrefix string, reg *runtime.Registry) (*Manager, error) {
	mng := &Manager{
		store:      store,
		runtimeReg: reg,
		pushPrefix: pushPrefix,
	}

	push, err := reg.Register(pushPrefix, runtime.SimpleValueGetterFunc(mng.runtimeGet))
	if err != nil {
		return nil, err
	}
	mng.push = push

	return mng, nil
}

func (mng *Manager) runtimeGet(keyOrPrefix string) ([]record.Record, error) {
	// TODO(ppacher):
	//		we don't yet support querying using the runtime database here ...
	//		consider exposing connection from the database at least by ID.
	//
	// NOTE(ppacher):
	//		for debugging purposes use RuntimeQueryRunner to execute plain
	//		SQL queries against the database using portbase/database/runtime.
	return nil, nil
}

// HandleFeed starts reading new and updated connections from feed and persists them
// in the configured ConnectionStore. HandleFeed blocks until either ctx is cancelled
// or feed is closed.
// Any errors encountered when processing new or updated connections are logged but
// otherwise ignored.
// HandleFeed handles and persists updates one after each other! Depending on the system
// load the user might want to use a buffered channel for feed.
func (mng *Manager) HandleFeed(ctx context.Context, feed <-chan *network.Connection) {
	// count the number of inserted rows for logging purposes.
	//
	// TODO(ppacher): how to handle the, though unlikely case, of a
	// overflow to 0 here?
	var count uint64

	for {
		select {
		case <-ctx.Done():
			return

		case conn, ok := <-feed:
			if !ok {
				return
			}

			model, err := convertConnection(conn)
			if err != nil {
				log.Errorf("netquery: failed to convert connection %s to sqlite model: %w", conn.ID, err)

				continue
			}

			log.Infof("netquery: persisting create/update to connection %s", conn.ID)

			if err := mng.store.Save(ctx, *model); err != nil {
				log.Errorf("netquery: failed to save connection %s in sqlite database: %w", conn.ID, err)

				continue
			}

			// we clone the record metadata from the connection
			// into the new model so the portbase/database layer
			// can handle NEW/UPDATE correctly.
			cloned := conn.Meta().Duplicate()

			// push an update for the connection
			if err := mng.pushConnUpdate(ctx, *cloned, *model); err != nil {
				log.Errorf("netquery: failed to push update for conn %s via database system: %w", conn.ID, err)
			}

			count++

			if count%20 == 0 {
				log.Debugf("netquery: persisted %d connections so far", count)
			}
		}
	}
}

func (mng *Manager) pushConnUpdate(ctx context.Context, meta record.Meta, conn Conn) error {
	blob, err := json.Marshal(conn)
	if err != nil {
		return fmt.Errorf("failed to marshal connection: %w", err)
	}

	key := fmt.Sprintf("%s:%s%s", mng.runtimeReg.DatabaseName(), mng.pushPrefix, conn.ID)
	wrapper, err := record.NewWrapper(
		key,
		&meta,
		dsd.JSON,
		blob,
	)
	if err != nil {
		return fmt.Errorf("failed to create record wrapper: %w", err)
	}

	mng.push(wrapper)
	return nil
}

// convertConnection converts conn to the local representation used
// to persist the information in SQLite. convertConnection attempts
// to lock conn and may thus block for some time.
func convertConnection(conn *network.Connection) (*Conn, error) {
	conn.Lock()
	defer conn.Unlock()

	c := Conn{
		ID:         genConnID(conn),
		External:   conn.External,
		IPVersion:  conn.IPVersion,
		IPProtocol: conn.IPProtocol,
		LocalIP:    conn.LocalIP.String(),
		LocalPort:  conn.LocalPort,
		Verdict:    conn.Verdict,
		Started:    time.Unix(conn.Started, 0),
		Tunneled:   conn.Tunneled,
		Encrypted:  conn.Encrypted,
		Internal:   conn.Internal,
		Inbound:    conn.Inbound,
		Type:       ConnectionTypeToString[conn.Type],
	}

	if conn.Ended > 0 {
		ended := time.Unix(conn.Ended, 0)
		c.Ended = &ended
	}

	extraData := map[string]interface{}{}

	if conn.Entity != nil {
		extraData["cname"] = conn.Entity.CNAME
		extraData["blockedByLists"] = conn.Entity.BlockedByLists
		extraData["blockedEntities"] = conn.Entity.BlockedEntities
		extraData["reason"] = conn.Reason

		c.RemoteIP = conn.Entity.IP.String()
		c.RemotePort = conn.Entity.Port
		c.Domain = conn.Entity.Domain
		c.Country = conn.Entity.Country
		c.ASN = conn.Entity.ASN
		c.ASOwner = conn.Entity.ASOrg
		c.Scope = conn.Entity.IPScope
		if conn.Entity.Coordinates != nil {
			c.Latitude = conn.Entity.Coordinates.Latitude
			c.Longitude = conn.Entity.Coordinates.Longitude
		}
	}

	// pre-compute the JSON blob for the extra data column
	// and assign it.
	extraDataBlob, err := json.Marshal(extraData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal extra data: %w", err)
	}
	c.ExtraData = extraDataBlob

	return &c, nil
}

func genConnID(conn *network.Connection) string {
	data := conn.ID + "-" + time.Unix(conn.Started, 0).String()
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
