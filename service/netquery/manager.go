package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/structures/dsd"
)

type (
	// ConnectionStore describes the interface that is used by Manager
	// to save new or updated connection objects.
	// It is implemented by the *Database type of this package.
	ConnectionStore interface {
		// Save is called to perists the new or updated connection. If required,
		// It's up to the implementation to figure out if the operation is an
		// insert or an update.
		// The ID of Conn is unique and can be trusted to never collide with other
		// connections of the save device.
		Save(ctx context.Context, conn Conn, history bool) error

		// MarkAllHistoryConnectionsEnded marks all active connections in the history
		// database as ended NOW.
		MarkAllHistoryConnectionsEnded(ctx context.Context) error

		// RemoveAllHistoryData removes all connections from the history database.
		RemoveAllHistoryData(ctx context.Context) error

		// RemoveHistoryForProfile removes all connections from the history database.
		// for a given profile ID (source/id)
		RemoveHistoryForProfile(ctx context.Context, profile string) error

		// UpdateBandwidth updates bandwidth data for the connection and optionally also writes
		// the bandwidth data to the history database.
		UpdateBandwidth(ctx context.Context, enableHistory bool, profileKey string, processKey string, connID string, bytesReceived uint64, bytesSent uint64) error

		// CleanupHistory deletes data outside of the retention time frame from the history database.
		CleanupHistory(ctx context.Context) error

		// Close closes the connection store. It must not be used afterwards.
		Close() error
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
	for {
		select {
		case <-ctx.Done():
			return

		case conn, ok := <-feed:
			if !ok {
				return
			}

			func() {
				conn.Lock()
				defer conn.Unlock()

				if !conn.DataIsComplete() {
					return
				}

				model, err := convertConnection(conn)
				if err != nil {
					log.Errorf("netquery: failed to convert connection %s to sqlite model: %s", conn.ID, err)

					return
				}

				// DEBUG:
				// log.Tracef("netquery: updating connection %s", conn.ID)

				// Save to netquery database.
				// Do not include internal connections in history.
				if err := mng.store.Save(ctx, *model, conn.HistoryEnabled); err != nil {
					log.Errorf("netquery: failed to save connection %s in sqlite database: %s", conn.ID, err)
					return
				}

				// we clone the record metadata from the connection
				// into the new model so the portbase/database layer
				// can handle NEW/UPDATE correctly.
				cloned := conn.Meta().Duplicate()

				// push an update for the connection
				if err := mng.pushConnUpdate(ctx, *cloned, *model); err != nil {
					log.Errorf("netquery: failed to push update for conn %s via database system: %s", conn.ID, err)
				}
			}()
		}
	}
}

func (mng *Manager) pushConnUpdate(_ context.Context, meta record.Meta, conn Conn) error {
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
// to persist the information in SQLite.
// The caller must hold the lock to the given network.Connection.
func convertConnection(conn *network.Connection) (*Conn, error) {
	direction := "outbound"
	if conn.Inbound {
		direction = "inbound"
	}

	c := Conn{
		ID:              makeNqIDFromConn(conn),
		External:        conn.External,
		IPVersion:       conn.IPVersion,
		IPProtocol:      conn.IPProtocol,
		LocalIP:         conn.LocalIP.String(),
		LocalPort:       conn.LocalPort,
		ActiveVerdict:   conn.Verdict,
		Started:         time.Unix(conn.Started, 0),
		Tunneled:        conn.Tunneled,
		Encrypted:       conn.Encrypted,
		Internal:        conn.Internal,
		Direction:       direction,
		Type:            ConnectionTypeToString[conn.Type],
		ProfileID:       conn.ProcessContext.Source + "/" + conn.ProcessContext.Profile,
		Path:            conn.ProcessContext.BinaryPath,
		ProfileRevision: int(conn.ProfileRevisionCounter),
		ProfileName:     conn.ProcessContext.ProfileName,
	}

	switch conn.Type {
	case network.DNSRequest:
		c.Type = "dns"
	case network.IPConnection:
		c.Type = "ip"
	case network.Undefined:
		c.Type = ""
	}

	c.Allowed = &conn.ConnectionEstablished

	if conn.Ended > 0 {
		ended := time.Unix(conn.Ended, 0)
		c.Ended = &ended
		c.Active = false
	} else {
		c.Active = true
	}

	extraData := map[string]interface{}{
		"pid":              conn.ProcessContext.PID,
		"processCreatedAt": conn.ProcessContext.CreatedAt,
	}

	if conn.TunnelContext != nil {
		extraData["tunnel"] = conn.TunnelContext
		exitNode := conn.TunnelContext.GetExitNodeID()
		c.ExitNode = &exitNode
	}

	if conn.DNSContext != nil {
		extraData["dns"] = conn.DNSContext
	}

	// TODO(ppacher): enable when TLS inspection is merged
	// if conn.TLSContext != nil {
	// 	extraData["tls"] = conn.TLSContext
	// }

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

// makeNqIDFromConn creates a netquery connection ID from the network connection.
func makeNqIDFromConn(conn *network.Connection) string {
	return makeNqIDFromParts(conn.Process().GetKey(), conn.ID)
}

// makeNqIDFromParts creates a netquery connection ID from the given network
// connection ID and the process key.
func makeNqIDFromParts(processKey string, netConnID string) string {
	return processKey + "-" + netConnID
}
