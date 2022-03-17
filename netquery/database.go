package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netquery/orm"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// InMemory is the "file path" to open a new in-memory database.
const InMemory = ":memory:"

// Available connection types as their string representation.
const (
	ConnTypeDNS = "dns"
	ConnTypeIP  = "ip"
)

// ConnectionTypeToString is a lookup map to get the string representation
// of a network.ConnectionType as used by this package.
var ConnectionTypeToString = map[network.ConnectionType]string{
	network.DNSRequest:   ConnTypeDNS,
	network.IPConnection: ConnTypeIP,
}

type (
	// Database represents a SQLite3 backed connection database.
	// It's use is tailored for persistance and querying of network.Connection.
	// Access to the underlying SQLite database is synchronized.
	//
	// TODO(ppacher): somehow I'm receiving SIGBUS or SIGSEGV when no doing
	// synchronization in *Database. Check what exactly sqlite.OpenFullMutex, etc..
	// are actually supposed to do.
	//
	Database struct {
		l    sync.Mutex
		conn *sqlite.Conn
	}

	// Conn is a network connection that is stored in a SQLite database and accepted
	// by the *Database type of this package. This also defines, using the ./orm package,
	// the table schema and the model that is exposed via the runtime database as well as
	// the query API.
	//
	// Use ConvertConnection from this package to convert a network.Connection to this
	// representation.
	Conn struct {
		// ID is a device-unique identifier for the connection. It is built
		// from network.Connection by hashing the connection ID and the start
		// time. We cannot just use the network.Connection.ID because it is only unique
		// as long as the connection is still active and might be, although unlikely,
		// reused afterwards.
		ID         string            `sqlite:"id,primary"`
		Type       string            `sqlite:"type,varchar(8)"`
		External   bool              `sqlite:"external"`
		IPVersion  packet.IPVersion  `sqlite:"ip_version"`
		IPProtocol packet.IPProtocol `sqlite:"ip_protocol"`
		LocalIP    string            `sqlite:"local_ip"`
		LocalPort  uint16            `sqlite:"local_port"`
		RemoteIP   string            `sqlite:"remote_ip"`
		RemotePort uint16            `sqlite:"remote_port"`
		Domain     string            `sqlite:"domain"`
		Country    string            `sqlite:"country,varchar(2)"`
		ASN        uint              `sqlite:"asn"`
		ASOwner    string            `sqlite:"as_owner"`
		Latitude   float64           `sqlite:"latitude"`
		Longitude  float64           `sqlite:"longitude"`
		Scope      netutils.IPScope  `sqlite:"scope"`
		Verdict    network.Verdict   `sqlite:"verdict"`
		Started    time.Time         `sqlite:"started,text"`
		Ended      *time.Time        `sqlite:"ended,text"`
		Tunneled   bool              `sqlite:"tunneled"`
		Encrypted  bool              `sqlite:"encrypted"`
		Internal   bool              `sqlite:"internal"`
		Inbound    bool              `sqlite:"inbound"`
		ExtraData  json.RawMessage   `sqlite:"extra_data"`
	}
)

// New opens a new database at path. The database is opened with Full-Mutex, Write-Ahead-Log (WAL)
// and Shared-Cache enabled.
//
// TODO(ppacher): check which sqlite "open flags" provide the best performance and don't cause
// SIGBUS/SIGSEGV when used with out a dedicated mutex in *Database.
//
func New(path string) (*Database, error) {
	c, err := sqlite.OpenConn(
		path,
		sqlite.OpenCreate,
		sqlite.OpenReadWrite,
		sqlite.OpenFullMutex,
		sqlite.OpenWAL,
		sqlite.OpenSharedCache,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite at %s: %w", path, err)
	}

	return &Database{conn: c}, nil
}

// NewInMemory is like New but creates a new in-memory database and
// automatically applies the connection table schema.
func NewInMemory() (*Database, error) {
	db, err := New(InMemory)
	if err != nil {
		return nil, err
	}

	// this should actually never happen because an in-memory database
	// always starts empty...
	if err := db.ApplyMigrations(); err != nil {
		return nil, fmt.Errorf("failed to prepare database: %w", err)
	}

	return db, nil
}

// ApplyMigrations applies any table and data migrations that are needed
// to bring db up-to-date with the built-in schema.
// TODO(ppacher): right now this only applies the current schema and ignores
// any data-migrations. Once the history module is implemented this should
// become/use a full migration system -- use zombiezen.com/go/sqlite/sqlitemigration
func (db *Database) ApplyMigrations() error {
	schema, err := orm.GenerateTableSchema("connections", Conn{})
	if err != nil {
		return fmt.Errorf("failed to generate table schema for conncetions: %w", err)
	}

	// get the create-table SQL statement from the infered schema
	sql := schema.CreateStatement(false)

	// execute the SQL
	if err := sqlitex.ExecuteTransient(db.conn, sql, nil); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// Execute executes a custom SQL query against the SQLite database used by db.
// It uses orm.RunQuery() under the hood so please refer to the orm package for
// more information about available options.
func (db *Database) Execute(ctx context.Context, sql string, args ...orm.QueryOption) error {
	db.l.Lock()
	defer db.l.Unlock()

	return orm.RunQuery(ctx, db.conn, sql, args...)
}

// CountRows returns the number of rows stored in the database.
func (db *Database) CountRows(ctx context.Context) (int, error) {
	var result []struct {
		Count int `sqlite:"count"`
	}

	if err := db.Execute(ctx, "SELECT COUNT(*) AS count FROM connections", orm.WithResult(&result)); err != nil {
		return 0, fmt.Errorf("failed to perform query: %w", err)
	}

	if len(result) != 1 {
		return 0, fmt.Errorf("unexpected number of rows returned, expected 1 got %d", len(result))
	}

	return result[0].Count, nil
}

// Cleanup removes all connections that have ended before threshold.
//
// NOTE(ppacher): there is no easy way to get the number of removed
// rows other than counting them in a first step. Though, that's
// probably not worth the cylces...
func (db *Database) Cleanup(ctx context.Context, threshold time.Time) (int, error) {
	where := `WHERE ended IS NOT NULL
			AND datetime(ended) < :threshold`
	sql := "DELETE FROM connections " + where + ";"

	args := orm.WithNamedArgs(map[string]interface{}{
		":threshold": threshold,
	})

	var result []struct {
		Count int `sqlite:"count"`
	}
	if err := db.Execute(
		ctx,
		"SELECT COUNT(*) AS count FROM connections "+where,
		args,
		orm.WithTransient(),
		orm.WithResult(&result),
	); err != nil {
		return 0, fmt.Errorf("failed to perform query: %w", err)
	}
	if len(result) != 1 {
		return 0, fmt.Errorf("unexpected number of rows, expected 1 got %d", len(result))
	}

	err := db.Execute(ctx, sql, args)
	if err != nil {
		return 0, err
	}

	return result[0].Count, nil
}

// dumpTo is a simple helper method that dumps all rows stored in the SQLite database
// as JSON to w.
// Any error aborts dumping rows and is returned.
func (db *Database) dumpTo(ctx context.Context, w io.Writer) error {
	db.l.Lock()
	defer db.l.Unlock()

	var conns []Conn
	if err := sqlitex.ExecuteTransient(db.conn, "SELECT * FROM connections", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			var c Conn
			if err := orm.DecodeStmt(ctx, stmt, &c, orm.DefaultDecodeConfig); err != nil {
				return err
			}

			conns = append(conns, c)
			return nil
		},
	}); err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(conns)
}

// Save inserts the connection conn into the SQLite database. If conn
// already exists the table row is updated instead.
func (db *Database) Save(ctx context.Context, conn Conn) error {
	connMap, err := orm.ToParamMap(ctx, conn, "", orm.DefaultEncodeConfig)
	if err != nil {
		return fmt.Errorf("failed to encode connection for SQL: %w", err)
	}

	columns := make([]string, 0, len(connMap))
	placeholders := make([]string, 0, len(connMap))
	values := make(map[string]interface{}, len(connMap))
	updateSets := make([]string, 0, len(connMap))

	for key, value := range connMap {
		columns = append(columns, key)
		placeholders = append(placeholders, ":"+key)
		values[":"+key] = value
		updateSets = append(updateSets, fmt.Sprintf("%s = :%s", key, key))
	}

	db.l.Lock()
	defer db.l.Unlock()

	// TODO(ppacher): make sure this one can be cached to speed up inserting
	// and save some CPU cycles for the user
	sql := fmt.Sprintf(
		`INSERT INTO connections (%s)
			VALUES(%s)
			ON CONFLICT(id) DO UPDATE SET
			%s
		`,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
		strings.Join(updateSets, ", "),
	)

	if err := sqlitex.ExecuteTransient(db.conn, sql, &sqlitex.ExecOptions{
		Named: values,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			log.Errorf("netquery: got result statement with %d columns", stmt.ColumnCount())
			return nil
		},
	}); err != nil {
		log.Errorf("netquery: failed to execute: %s", err)
		return err
	}

	return nil
}

// Close closes the underlying database connection. db should and cannot be
// used after Close() has returned.
func (db *Database) Close() error {
	return db.conn.Close()
}
