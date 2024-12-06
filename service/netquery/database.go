package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/jackc/puddle/v2"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/netquery/orm"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/profile"
)

// InMemory is the "file path" to open a new in-memory database.
const InMemory = "file:inmem.db?mode=memory"

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
		historyPath  string

		l         sync.Mutex
		writeConn *sqlite.Conn
	}

	// BatchExecute executes multiple queries in one transaction.
	BatchExecute struct {
		ID     string
		SQL    string
		Params map[string]any
		Result *[]map[string]any
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
		WorstVerdict    network.Verdict   `sqlite:"worst_verdict"`
		ActiveVerdict   network.Verdict   `sqlite:"verdict"`
		FirewallVerdict network.Verdict   `sqlite:"firewall_verdict"`
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
		BytesReceived   uint64            `sqlite:"bytes_received,default=0"`
		BytesSent       uint64            `sqlite:"bytes_sent,default=0"`

		// TODO(ppacher): support "NOT" in search query to get rid of the following helper fields
		Active bool `sqlite:"active"` // could use "ended IS NOT NULL" or "ended IS NULL"

		// TODO(ppacher): we need to profile here for "suggestion" support. It would be better to keep a table of profiles in sqlite and use joins here
		ProfileName string `sqlite:"profile_name"`
	}
)

// New opens a new in-memory database named path and attaches a persistent history database.
//
// The returned Database used connection pooling for read-only connections
// (see Execute). To perform database writes use either Save() or ExecuteWrite().
// Note that write connections are serialized by the Database object before being
// handed over to SQLite.
func New(dbPath string) (*Database, error) {
	historyParentDir := dataroot.Root().ChildDir("databases", utils.AdminOnlyPermission)
	if err := historyParentDir.Ensure(); err != nil {
		return nil, fmt.Errorf("failed to ensure database directory exists: %w", err)
	}

	// Get file location of history database.
	historyFile := filepath.Join(historyParentDir.Path, "history.db")
	// Convert to SQLite URI path.
	historyURI := "file:///" + strings.TrimPrefix(filepath.ToSlash(historyFile), "/")

	constructor := func(ctx context.Context) (*sqlite.Conn, error) {
		c, err := sqlite.OpenConn(
			dbPath,
			sqlite.OpenReadOnly,
			sqlite.OpenSharedCache,
			sqlite.OpenURI,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to open read-only sqlite connection at %s: %w", dbPath, err)
		}

		if err := sqlitex.ExecuteTransient(c, "ATTACH DATABASE '"+historyURI+"?mode=ro' AS history", nil); err != nil {
			return nil, fmt.Errorf("failed to attach history database: %w", err)
		}

		return c, nil
	}

	destructor := func(resource *sqlite.Conn) {
		if err := resource.Close(); err != nil {
			log.Errorf("failed to close pooled SQlite database connection: %s", err)
		}
	}

	pool, err := puddle.NewPool(&puddle.Config[*sqlite.Conn]{
		Constructor: constructor,
		Destructor:  destructor,
		MaxSize:     10,
	})
	if err != nil {
		return nil, err
	}

	schema, err := orm.GenerateTableSchema("connections", Conn{})
	if err != nil {
		return nil, err
	}

	writeConn, err := sqlite.OpenConn(
		dbPath,
		sqlite.OpenCreate,
		sqlite.OpenReadWrite,
		sqlite.OpenWAL,
		sqlite.OpenSharedCache,
		sqlite.OpenURI,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite at %s: %w", dbPath, err)
	}

	return &Database{
		readConnPool: pool,
		Schema:       schema,
		writeConn:    writeConn,
		historyPath:  historyURI,
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

// Close closes the database, including pools and connections.
func (db *Database) Close() error {
	db.readConnPool.Close()

	if err := db.writeConn.Close(); err != nil {
		return err
	}

	return nil
}

// VacuumHistory rewrites the history database in order to purge deleted records.
func VacuumHistory(ctx context.Context) (err error) {
	historyParentDir := dataroot.Root().ChildDir("databases", utils.AdminOnlyPermission)
	if err := historyParentDir.Ensure(); err != nil {
		return fmt.Errorf("failed to ensure database directory exists: %w", err)
	}

	// Get file location of history database.
	historyFile := filepath.Join(historyParentDir.Path, "history.db")
	// Convert to SQLite URI path.
	historyURI := "file:///" + strings.TrimPrefix(filepath.ToSlash(historyFile), "/")

	writeConn, err := sqlite.OpenConn(
		historyURI,
		sqlite.OpenCreate,
		sqlite.OpenReadWrite,
		sqlite.OpenWAL,
		sqlite.OpenSharedCache,
		sqlite.OpenURI,
	)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := writeConn.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	return orm.RunQuery(ctx, writeConn, "VACUUM")
}

// ApplyMigrations applies any table and data migrations that are needed
// to bring db up-to-date with the built-in schema.
// TODO(ppacher): right now this only applies the current schema and ignores
// any data-migrations. Once the history module is implemented this should
// become/use a full migration system -- use zombiezen.com/go/sqlite/sqlitemigration.
func (db *Database) ApplyMigrations() error {
	db.l.Lock()
	defer db.l.Unlock()

	if err := sqlitex.ExecuteTransient(db.writeConn, "ATTACH DATABASE '"+db.historyPath+"?mode=rwc' AS 'history';", nil); err != nil {
		return fmt.Errorf("failed to attach history database: %w", err)
	}

	dbNames := []string{"main", "history"}
	for _, dbName := range dbNames {
		// get the create-table SQL statement from the inferred schema
		sql := db.Schema.CreateStatement(dbName, true)
		log.Debugf("creating table schema for database %q", dbName)

		// execute the SQL
		if err := sqlitex.ExecuteTransient(db.writeConn, sql, nil); err != nil {
			return fmt.Errorf("failed to create schema on database %q: %w", dbName, err)
		}

		// create a few indexes
		indexes := []string{
			`CREATE INDEX IF NOT EXISTS %sprofile_id_index ON %s (profile)`,
			`CREATE INDEX IF NOT EXISTS %sstarted_time_index ON %s (strftime('%%s', started)+0)`,
			`CREATE INDEX IF NOT EXISTS %sstarted_ended_time_index ON %s (strftime('%%s', started)+0, strftime('%%s', ended)+0) WHERE ended IS NOT NULL`,
		}
		for _, idx := range indexes {
			name := ""
			if dbName != "" {
				name = dbName + "."
			}

			stmt := fmt.Sprintf(idx, name, db.Schema.Name)

			if err := sqlitex.ExecuteTransient(db.writeConn, stmt, nil); err != nil {
				return fmt.Errorf("failed to create index on database %q: %q: %w", dbName, idx, err)
			}
		}
	}

	bwSchema := `CREATE TABLE IF NOT EXISTS main.bandwidth (
		conn_id TEXT NOT NULL,
		time INTEGER NOT NULL,
		incoming INTEGER NOT NULL,
		outgoing INTEGER NOT NULL,
		CONSTRAINT fk_conn_id
			FOREIGN KEY(conn_id) REFERENCES connections(id)
			ON DELETE CASCADE
	)`
	if err := sqlitex.ExecuteTransient(db.writeConn, bwSchema, nil); err != nil {
		return fmt.Errorf("failed to create main.bandwidth database: %w", err)
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

// ExecuteBatch executes multiple custom SQL query using a read-only connection against the SQLite
// database used by db.
func (db *Database) ExecuteBatch(ctx context.Context, batches []BatchExecute) error {
	return db.withConn(ctx, func(conn *sqlite.Conn) error {
		merr := new(multierror.Error)

		for _, batch := range batches {
			if err := orm.RunQuery(ctx, conn, batch.SQL, orm.WithNamedArgs(batch.Params), orm.WithResult(batch.Result)); err != nil {
				merr.Errors = append(merr.Errors, fmt.Errorf("%s: %w", batch.ID, err))
			}
		}

		return merr.ErrorOrNil()
	})
}

// CountRows returns the number of rows stored in the database.
func (db *Database) CountRows(ctx context.Context) (int, error) {
	var result []struct {
		Count int `sqlite:"count"`
	}

	if err := db.Execute(ctx, "SELECT COUNT(*) AS count FROM (SELECT * FROM main.connections UNION SELECT * from history.connections)", orm.WithResult(&result)); err != nil {
		return 0, fmt.Errorf("failed to perform query: %w", err)
	}

	if len(result) != 1 {
		return 0, fmt.Errorf("unexpected number of rows returned, expected 1 got %d", len(result))
	}

	return result[0].Count, nil
}

// Cleanup removes all connections that have ended before threshold from the live database.
//
// NOTE(ppacher): there is no easy way to get the number of removed
// rows other than counting them in a first step. Though, that's
// probably not worth the cylces...
func (db *Database) Cleanup(ctx context.Context, threshold time.Time) (int, error) {
	where := `WHERE ended IS NOT NULL
			AND datetime(ended) < datetime(:threshold)`
	sql := "DELETE FROM main.connections " + where + ";"

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

// RemoveAllHistoryData removes all connections from the history database.
func (db *Database) RemoveAllHistoryData(ctx context.Context) error {
	query := fmt.Sprintf("DELETE FROM %s.connections", HistoryDatabase)
	return db.ExecuteWrite(ctx, query)
}

// RemoveHistoryForProfile removes all connections from the history database
// for a given profile ID (source/id).
func (db *Database) RemoveHistoryForProfile(ctx context.Context, profileID string) error {
	query := fmt.Sprintf("DELETE FROM %s.connections WHERE profile = :profile", HistoryDatabase)
	return db.ExecuteWrite(ctx, query, orm.WithNamedArgs(map[string]any{
		":profile": profileID,
	}))
}

// MigrateProfileID migrates the given profile IDs in the history database.
// This needs to be done when profiles are deleted and replaced by a different profile.
func (db *Database) MigrateProfileID(ctx context.Context, from string, to string) error {
	return db.ExecuteWrite(ctx, "UPDATE history.connections SET profile = :to WHERE profile = :from", orm.WithNamedArgs(map[string]any{
		":from": from,
		":to":   to,
	}))
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

// CleanupHistory deletes history data outside of the (per-app) retention time frame.
func (db *Database) CleanupHistory(ctx context.Context) error {
	// Setup tracer for the clean up process.
	ctx, tracer := log.AddTracer(ctx)
	defer tracer.Submit()

	// Get list of profiles in history.
	query := "SELECT DISTINCT profile FROM history.connections"
	var result []struct {
		Profile string `sqlite:"profile"`
	}
	if err := db.Execute(ctx, query, orm.WithResult(&result)); err != nil {
		return fmt.Errorf("failed to get a list of profiles from the history database: %w", err)
	}

	var (
		// Get global retention days - do not delete in case of error.
		globalRetentionDays = config.GetAsInt(profile.CfgOptionKeepHistoryKey, 0)()

		profileName   string
		retentionDays int64

		profileCnt int
		merr       = new(multierror.Error)
	)
	for _, row := range result {
		// Get profile and retention days.
		id := strings.TrimPrefix(row.Profile, string(profile.SourceLocal)+"/")
		p, err := profile.GetLocalProfile(id, nil, nil)
		if err == nil {
			profileName = p.String()
			retentionDays = p.LayeredProfile().KeepHistory()
		} else {
			// Getting profile failed, fallback to global setting.
			tracer.Errorf("history: failed to load profile for id %s: %s", id, err)
			profileName = row.Profile
			retentionDays = globalRetentionDays
		}

		// Skip deleting if history should be kept forever.
		if retentionDays == 0 {
			tracer.Tracef("history: retention is disabled for %s, skipping", profileName)
			continue
		}
		// Count profiles where connections were deleted.
		profileCnt++

		// TODO: count cleared connections
		threshold := time.Now().Add(-1 * time.Duration(retentionDays) * time.Hour * 24)
		if err := db.ExecuteWrite(ctx,
			"DELETE FROM history.connections WHERE profile = :profile AND active = FALSE AND datetime(started) < datetime(:threshold)",
			orm.WithNamedArgs(map[string]any{
				":profile":   row.Profile,
				":threshold": threshold.Format(orm.SqliteTimeFormat),
			}),
		); err != nil {
			tracer.Warningf("history: failed to delete connections of %s: %s", profileName, err)
			merr.Errors = append(merr.Errors, fmt.Errorf("profile %s: %w", row.Profile, err))
		} else {
			tracer.Debugf(
				"history: deleted connections older than %d days (before %s) of %s",
				retentionDays,
				threshold.Format(time.RFC822),
				profileName,
			)
		}
	}

	// Log summary.
	tracer.Infof("history: deleted connections outside of retention from %d profiles", profileCnt)

	return merr.ErrorOrNil()
}

// MarkAllHistoryConnectionsEnded marks all connections in the history database as ended.
func (db *Database) MarkAllHistoryConnectionsEnded(ctx context.Context) error {
	query := fmt.Sprintf("UPDATE %s.connections SET active = FALSE, ended = :ended WHERE active = TRUE", HistoryDatabase)

	if err := db.ExecuteWrite(ctx, query, orm.WithNamedArgs(map[string]any{
		":ended": time.Now().Format(orm.SqliteTimeFormat),
	})); err != nil {
		return err
	}

	return nil
}

// UpdateBandwidth updates bandwidth data for the connection and optionally also writes
// the bandwidth data to the history database.
func (db *Database) UpdateBandwidth(ctx context.Context, enableHistory bool, profileKey string, processKey string, connID string, bytesReceived uint64, bytesSent uint64) error {
	params := map[string]any{
		":id": makeNqIDFromParts(processKey, connID),
	}

	parts := []string{}
	parts = append(parts, "bytes_received = (bytes_received + :bytes_received)")
	params[":bytes_received"] = bytesReceived
	parts = append(parts, "bytes_sent = (bytes_sent + :bytes_sent)")
	params[":bytes_sent"] = bytesSent

	updateSet := strings.Join(parts, ", ")

	updateStmts := []string{
		fmt.Sprintf(`UPDATE %s.connections SET %s WHERE id = :id`, LiveDatabase, updateSet),
	}

	if enableHistory {
		updateStmts = append(updateStmts,
			fmt.Sprintf(`UPDATE %s.connections SET %s WHERE id = :id`, HistoryDatabase, updateSet),
		)
	}

	merr := new(multierror.Error)
	for _, stmt := range updateStmts {
		if err := db.ExecuteWrite(ctx, stmt, orm.WithNamedArgs(params)); err != nil {
			merr.Errors = append(merr.Errors, err)
		}
	}

	// also add the date to the in-memory bandwidth database
	params[":time"] = time.Now().Unix()
	stmt := "INSERT INTO main.bandwidth (conn_id, time, incoming, outgoing) VALUES(:id, :time, :bytes_received, :bytes_sent)"
	if err := db.ExecuteWrite(ctx, stmt, orm.WithNamedArgs(params)); err != nil {
		merr.Errors = append(merr.Errors, fmt.Errorf("failed to update main.bandwidth: %w", err))
	}

	return merr.ErrorOrNil()
}

// Save inserts the connection conn into the SQLite database. If conn
// already exists the table row is updated instead.
//
// Save uses the database write connection instead of relying on the
// connection pool.
func (db *Database) Save(ctx context.Context, conn Conn, enableHistory bool) error {
	// convert the connection to a param map where each key is already translated
	// to the sql column name. We also skip bytes_received and bytes_sent since those
	// will be updated independently from the connection object.
	connMap, err := orm.ToParamMap(ctx, conn, "", orm.DefaultEncodeConfig, []string{
		"bytes_received",
		"bytes_sent",
	})
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
	dbNames := []DatabaseName{LiveDatabase}

	// TODO: Should we only add ended connection to the history database to save
	// a couple INSERTs per connection?
	// This means we need to write the current live DB to the history DB on
	// shutdown in order to be able to pick the back up after a restart.

	// Save to history DB if enabled.
	if enableHistory {
		dbNames = append(dbNames, HistoryDatabase)
	}

	for _, dbName := range dbNames {
		sql := fmt.Sprintf(
			`INSERT INTO %s.connections (%s)
				VALUES(%s)
				ON CONFLICT(id) DO UPDATE SET
				%s
			`,
			dbName,
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
	}

	return nil
}
