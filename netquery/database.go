package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/puddle/v2"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netquery/orm"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
)

// InMemory is the "file path" to open a new in-memory database.
const InMemory = "file:inmem.db"

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
	// It's use is tailored for persistence and querying of network.Connection.
	// Access to the underlying SQLite database is synchronized.
	//
	Database struct {
		Schema *orm.TableSchema

		readConnPool *puddle.Pool[*sqlite.Conn]

		l         sync.Mutex
		writeConn *sqlite.Conn
	}

	// Conn is a network connection that is stored in a SQLite database and accepted
	// by the *Database type of this package. This also defines, using the ./orm package,
	// the table schema and the model that is exposed via the runtime database as well as
	// the query API.
	//
	// Use ConvertConnection from this package to convert a network.Connection to this
	// representation.
	Conn struct { //nolint:maligned
		// ID is a device-unique identifier for the connection. It is built
		// from network.Connection by hashing the connection ID and the start
		// time. We cannot just use the network.Connection.ID because it is only unique
		// as long as the connection is still active and might be, although unlikely,
		// reused afterwards.
		ID              string            `sqlite:"id,primary"`
		ProfileID       string            `sqlite:"profile"`
		Path            string            `sqlite:"path"`
		Type            string            `sqlite:"type,varchar(8)"`
		External        bool              `sqlite:"external"`
		IPVersion       packet.IPVersion  `sqlite:"ip_version"`
		IPProtocol      packet.IPProtocol `sqlite:"ip_protocol"`
		LocalIP         string            `sqlite:"local_ip"`
		LocalPort       uint16            `sqlite:"local_port"`
		RemoteIP        string            `sqlite:"remote_ip"`
		RemotePort      uint16            `sqlite:"remote_port"`
		Domain          string            `sqlite:"domain"`
		Country         string            `sqlite:"country,varchar(2)"`
		ASN             uint              `sqlite:"asn"`
		ASOwner         string            `sqlite:"as_owner"`
		Latitude        float64           `sqlite:"latitude"`
		Longitude       float64           `sqlite:"longitude"`
		Scope           netutils.IPScope  `sqlite:"scope"`
		Verdict         network.Verdict   `sqlite:"verdict"`
		Started         time.Time         `sqlite:"started,text,time"`
		Ended           *time.Time        `sqlite:"ended,text,time"`
		Tunneled        bool              `sqlite:"tunneled"`
		Encrypted       bool              `sqlite:"encrypted"`
		Internal        bool              `sqlite:"internal"`
		Direction       string            `sqlite:"direction"`
		ExtraData       json.RawMessage   `sqlite:"extra_data"`
		Allowed         *bool             `sqlite:"allowed"`
		ProfileRevision int               `sqlite:"profile_revision"`
		ExitNode        *string           `sqlite:"exit_node"`

		// TODO(ppacher): support "NOT" in search query to get rid of the following helper fields
		Active bool `sqlite:"active"` // could use "ended IS NOT NULL" or "ended IS NULL"

		// TODO(ppacher): we need to profile here for "suggestion" support. It would be better to keep a table of profiles in sqlite and use joins here
		ProfileName string `sqlite:"profile_name"`
	}
)

// New opens a new in-memory database named path.
//
// The returned Database used connection pooling for read-only connections
// (see Execute). To perform database writes use either Save() or ExecuteWrite().
// Note that write connections are serialized by the Database object before being
// handed over to SQLite.
func New(path string) (*Database, error) {
	constructor := func(ctx context.Context) (*sqlite.Conn, error) {
		c, err := sqlite.OpenConn(
			path,
			sqlite.OpenReadOnly,
			sqlite.OpenNoMutex,
			sqlite.OpenSharedCache,
			sqlite.OpenMemory,
			sqlite.OpenURI,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to open read-only sqlite connection at %s: %w", path, err)
		}

		return c, nil
	}

	destructor := func(resource *sqlite.Conn) {
		if err := resource.Close(); err != nil {
			log.Errorf("failed to close pooled SQlite database connection: %s", err)
		}
	}

	pool := puddle.NewPool(constructor, destructor, 10)

	schema, err := orm.GenerateTableSchema("connections", Conn{})
	if err != nil {
		return nil, err
	}

	writeConn, err := sqlite.OpenConn(
		path,
		sqlite.OpenCreate,
		sqlite.OpenReadWrite,
		sqlite.OpenNoMutex,
		sqlite.OpenWAL,
		sqlite.OpenSharedCache,
		sqlite.OpenMemory,
		sqlite.OpenURI,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite at %s: %w", path, err)
	}

	return &Database{
		readConnPool: pool,
		Schema:       schema,
		writeConn:    writeConn,
	}, nil
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
// become/use a full migration system -- use zombiezen.com/go/sqlite/sqlitemigration.
func (db *Database) ApplyMigrations() error {
	// get the create-table SQL statement from the inferred schema
	sql := db.Schema.CreateStatement(true)

	db.l.Lock()
	defer db.l.Unlock()

	// execute the SQL
	if err := sqlitex.ExecuteTransient(db.writeConn, sql, nil); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// create a few indexes
	indexes := []string{
		`CREATE INDEX profile_id_index ON %s (profile)`,
		`CREATE INDEX started_time_index ON %s (strftime('%%s', started)+0)`,
		`CREATE INDEX started_ended_time_index ON %s (strftime('%%s', started)+0, strftime('%%s', ended)+0) WHERE ended IS NOT NULL`,
	}
	for _, idx := range indexes {
		stmt := fmt.Sprintf(idx, db.Schema.Name)

		if err := sqlitex.ExecuteTransient(db.writeConn, stmt, nil); err != nil {
			return fmt.Errorf("failed to create index: %q: %w", idx, err)
		}
	}

	return nil
}

func (db *Database) withConn(ctx context.Context, fn func(conn *sqlite.Conn) error) error {
	res, err := db.readConnPool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer res.Release()

	return fn(res.Value())
}

// ExecuteWrite executes a custom SQL query using a writable connection against the SQLite
// database used by db.
// It uses orm.RunQuery() under the hood so please refer to the orm package for
// more information about available options.
func (db *Database) ExecuteWrite(ctx context.Context, sql string, args ...orm.QueryOption) error {
	db.l.Lock()
	defer db.l.Unlock()

	return orm.RunQuery(ctx, db.writeConn, sql, args...)
}

// Execute executes a custom SQL query using a read-only connection against the SQLite
// database used by db.
// It uses orm.RunQuery() under the hood so please refer to the orm package for
// more information about available options.
func (db *Database) Execute(ctx context.Context, sql string, args ...orm.QueryOption) error {
	return db.withConn(ctx, func(conn *sqlite.Conn) error {
		return orm.RunQuery(ctx, conn, sql, args...)
	})
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
			AND datetime(ended) < datetime(:threshold)`
	sql := "DELETE FROM connections " + where + ";"

	args := orm.WithNamedArgs(map[string]interface{}{
		":threshold": threshold.UTC().Format(orm.SqliteTimeFormat),
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

	err := db.ExecuteWrite(ctx, sql, args)
	if err != nil {
		return 0, err
	}

	return result[0].Count, nil
}

// dumpTo is a simple helper method that dumps all rows stored in the SQLite database
// as JSON to w.
// Any error aborts dumping rows and is returned.
func (db *Database) dumpTo(ctx context.Context, w io.Writer) error { //nolint:unused
	var conns []Conn
	err := db.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.ExecuteTransient(conn, "SELECT * FROM connections", &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				var c Conn
				if err := orm.DecodeStmt(ctx, db.Schema, stmt, &c, orm.DefaultDecodeConfig); err != nil {
					return err
				}

				conns = append(conns, c)
				return nil
			},
		})
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(conns)
}

// Save inserts the connection conn into the SQLite database. If conn
// already exists the table row is updated instead.
//
// Save uses the database write connection instead of relying on the
// connection pool.
func (db *Database) Save(ctx context.Context, conn Conn) error {
	connMap, err := orm.ToParamMap(ctx, conn, "", orm.DefaultEncodeConfig)
	if err != nil {
		return fmt.Errorf("failed to encode connection for SQL: %w", err)
	}

	columns := make([]string, 0, len(connMap))
	placeholders := make([]string, 0, len(connMap))
	values := make(map[string]interface{}, len(connMap))
	updateSets := make([]string, 0, len(connMap))

	// sort keys so we get a stable SQLite query that can be better cached.
	keys := make([]string, 0, len(connMap))
	for key := range connMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := connMap[key]

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

	if err := sqlitex.Execute(db.writeConn, sql, &sqlitex.ExecOptions{
		Named: values,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			log.Errorf("netquery: got result statement with %d columns", stmt.ColumnCount())
			return nil
		},
	}); err != nil {
		log.Errorf("netquery: failed to execute:\n\t%q\n\treturned error was: %s\n\tparameters: %+v", sql, err, values)
		return err
	}

	return nil
}

// Close closes the underlying database connection. db should and cannot be
// used after Close() has returned.
func (db *Database) Close() error {
	return db.writeConn.Close()
}
