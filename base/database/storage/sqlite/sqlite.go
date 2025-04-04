package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/aarondl/opt/omit"
	"github.com/aarondl/opt/omitnull"
	migrate "github.com/rubenv/sql-migrate"
	sqldblogger "github.com/simukti/sqldb-logger"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/sqlite"
	"github.com/stephenafamo/bob/dialect/sqlite/im"
	"github.com/stephenafamo/bob/dialect/sqlite/um"
	_ "modernc.org/sqlite"

	"github.com/safing/portmaster/base/database/accessor"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/database/storage/sqlite/models"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/structures/dsd"
)

// Errors.
var (
	ErrQueryTimeout = errors.New("query timeout")
)

// SQLite storage.
type SQLite struct {
	name string

	db  *sql.DB
	bob bob.DB
	wg  sync.WaitGroup

	ctx       context.Context
	cancelCtx context.CancelFunc
}

func init() {
	_ = storage.Register("sqlite", func(name, location string) (storage.Interface, error) {
		return NewSQLite(name, location)
	})
}

// NewSQLite creates a sqlite database.
func NewSQLite(name, location string) (*SQLite, error) {
	return openSQLite(name, location, false)
}

// openSQLite creates a sqlite database.
func openSQLite(name, location string, printStmts bool) (*SQLite, error) {
	dbFile := filepath.Join(location, "db.sqlite")

	// Open database file.
	// Default settings:
	// _time_format = YYYY-MM-DDTHH:MM:SS.SSS
	// _txlock = deferred
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable statement printing.
	if printStmts {
		db = sqldblogger.OpenDriver(dbFile, db.Driver(), &statementLogger{})
	}

	// Set other settings.
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",   // Corruption safe write ahead log for txs.
		"PRAGMA synchronous=NORMAL;", // Best for WAL.
		"PRAGMA cache_size=-10000;",  // 10MB Cache.
		"PRAGMA busy_timeout=3000;",  // 3s (3000ms) timeout for locked tables.
	}
	for _, pragma := range pragmas {
		_, err := db.Exec(pragma)
		if err != nil {
			return nil, fmt.Errorf("failed to init sqlite with %s: %w", pragma, err)
		}
	}

	// Run migrations on database.
	n, err := migrate.Exec(db, "sqlite3", getMigrations(), migrate.Up)
	if err != nil {
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	log.Debugf("database/sqlite: ran %d migrations on %s database", n, name)

	// Return as bob database.
	ctx, cancelCtx := context.WithCancel(context.Background())
	return &SQLite{
		name:      name,
		db:        db,
		bob:       bob.NewDB(db),
		ctx:       ctx,
		cancelCtx: cancelCtx,
	}, nil
}

// Get returns a database record.
func (db *SQLite) Get(key string) (record.Record, error) {
	db.wg.Add(1)
	defer db.wg.Done()

	// Get record from database.
	r, err := models.FindRecord(db.ctx, db.bob, key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}

	// Return data in wrapper.
	return record.NewWrapperFromDatabase(
		db.name,
		key,
		getMeta(r),
		uint8(r.Format.GetOrZero()), //nolint:gosec // Values are within uint8.
		r.Value.GetOrZero(),
	)
}

// GetMeta returns the metadata of a database record.
func (db *SQLite) GetMeta(key string) (*record.Meta, error) {
	r, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	return r.Meta(), nil
}

// Put stores a record in the database.
func (db *SQLite) Put(r record.Record) (record.Record, error) {
	return db.putRecord(r, nil)
}

func (db *SQLite) putRecord(r record.Record, tx *bob.Tx) (record.Record, error) {
	db.wg.Add(1)
	defer db.wg.Done()

	// Lock record if in a transaction.
	if tx != nil {
		r.Lock()
		defer r.Unlock()
	}

	// Serialize to JSON.
	data, err := r.MarshalDataOnly(r, dsd.JSON)
	if err != nil {
		return nil, err
	}
	// Prepare for setter.
	setFormat := omitnull.From(int16(dsd.JSON))
	setData := omitnull.From(data)
	if len(data) == 0 {
		setFormat.Null()
		setData.Null()
	}

	// Create structure for insert.
	m := r.Meta()
	setter := models.RecordSetter{
		Key:        omit.From(r.DatabaseKey()),
		Format:     setFormat,
		Value:      setData,
		Created:    omit.From(m.Created),
		Modified:   omit.From(m.Modified),
		Expires:    omit.From(m.Expires),
		Deleted:    omit.From(m.Deleted),
		Secret:     omit.From(m.IsSecret()),
		Crownjewel: omit.From(m.IsCrownJewel()),
	}

	// Simulate upsert with custom selection on conflict.
	dbQuery := models.Records.Insert(
		&setter,
		im.OnConflict("key").DoUpdate(
			im.SetExcluded("format", "value", "created", "modified", "expires", "deleted", "secret", "crownjewel"),
		),
	)

	// Execute in transaction or directly.
	if tx != nil {
		_, err = dbQuery.Exec(db.ctx, tx)
	} else {
		_, err = dbQuery.Exec(db.ctx, db.bob)
	}
	if err != nil {
		return nil, err
	}

	return r, nil
}

// PutMany stores many records in the database.
func (db *SQLite) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	db.wg.Add(1)
	defer db.wg.Done()

	// Check if we should use prepared statement optimized inserting.
	if UsePreparedStatements {
		return db.putManyWithPreparedStmts(shadowDelete)
	}

	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)

	tx, err := db.bob.BeginTx(db.ctx, nil)
	if err != nil {
		errs <- err
		return batch, errs
	}

	// start handler
	go func() {
		// Read all put records.
	writeBatch:
		for {
			select {
			case r := <-batch:
				if r != nil {
					// Write record.
					_, err := db.putRecord(r, &tx)
					if err != nil {
						errs <- err
						break writeBatch
					}
				} else {
					// Finalize transcation.
					errs <- tx.Commit()
					return
				}

			case <-db.ctx.Done():
				break writeBatch
			}
		}

		// Rollback transaction.
		errs <- tx.Rollback()
	}()

	return batch, errs
}

// Delete deletes a record from the database.
func (db *SQLite) Delete(key string) error {
	db.wg.Add(1)
	defer db.wg.Done()

	toDelete := &models.Record{Key: key}
	return toDelete.Delete(db.ctx, db.bob)
}

// Query returns a an iterator for the supplied query.
func (db *SQLite) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	db.wg.Add(1)
	defer db.wg.Done()

	_, err := q.Check()
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	queryIter := iterator.New()

	go db.queryExecutor(queryIter, q, local, internal)
	return queryIter, nil
}

func (db *SQLite) queryExecutor(queryIter *iterator.Iterator, q *query.Query, local, internal bool) {
	db.wg.Add(1)
	defer db.wg.Done()

	// Build query.
	var recordQuery *sqlite.ViewQuery[*models.Record, models.RecordSlice]
	if q.DatabaseKeyPrefix() != "" {
		recordQuery = models.Records.View.Query(
			models.SelectWhere.Records.Key.Like(q.DatabaseKeyPrefix() + "%"),
		)
	} else {
		recordQuery = models.Records.View.Query()
	}

	// Get cursor to go over all records in the query.
	cursor, err := models.RecordsQuery.Cursor(recordQuery, db.ctx, db.bob)
	if err != nil {
		queryIter.Finish(err)
		return
	}
	defer func() {
		_ = cursor.Close()
	}()

recordsLoop:
	for cursor.Next() {
		// Get next record
		r, cErr := cursor.Get()
		if cErr != nil {
			err = fmt.Errorf("cursor error: %w", cErr)
			break recordsLoop
		}

		// Check if key matches.
		if !q.MatchesKey(r.Key) {
			continue recordsLoop
		}

		// Check Meta.
		m := getMeta(r)
		if !m.CheckValidity() ||
			!m.CheckPermission(local, internal) {
			continue recordsLoop
		}

		// Check Data.
		if q.HasWhereCondition() {
			if r.Format.IsNull() || r.Value.IsNull() {
				continue recordsLoop
			}

			jsonData := string(r.Value.GetOrZero())
			jsonAccess := accessor.NewJSONAccessor(&jsonData)
			if !q.MatchesAccessor(jsonAccess) {
				continue recordsLoop
			}
		}

		// Build database record.
		matched, _ := record.NewWrapperFromDatabase(
			db.name,
			r.Key,
			m,
			uint8(r.Format.GetOrZero()), //nolint:gosec // Values are within uint8.
			r.Value.GetOrZero(),
		)

		select {
		case <-queryIter.Done:
			break recordsLoop
		case queryIter.Next <- matched:
		default:
			select {
			case <-queryIter.Done:
				break recordsLoop
			case queryIter.Next <- matched:
			case <-time.After(1 * time.Second):
				err = ErrQueryTimeout
				break recordsLoop
			}
		}

	}

	queryIter.Finish(err)
}

// Purge deletes all records that match the given query. It returns the number of successful deletes and an error.
func (db *SQLite) Purge(ctx context.Context, q *query.Query, local, internal, shadowDelete bool) (int, error) {
	db.wg.Add(1)
	defer db.wg.Done()

	// Optimize for local and internal queries without where clause and without shadow delete.
	if local && internal && !shadowDelete && !q.HasWhereCondition() {
		// First count entries (SQLite does not support affected rows)
		n, err := models.Records.Query(
			models.SelectWhere.Records.Key.Like(q.DatabaseKeyPrefix()+"%"),
		).Count(db.ctx, db.bob)
		if err != nil || n == 0 {
			return int(n), err
		}

		// Delete entries.
		_, err = models.Records.Delete(
			models.DeleteWhere.Records.Key.Like(q.DatabaseKeyPrefix()+"%"),
		).Exec(db.ctx, db.bob)
		return int(n), err
	}

	// Optimize for local and internal queries without where clause, but with shadow delete.
	if local && internal && shadowDelete && !q.HasWhereCondition() {
		// First count entries (SQLite does not support affected rows)
		n, err := models.Records.Query(
			models.SelectWhere.Records.Key.Like(q.DatabaseKeyPrefix()+"%"),
		).Count(db.ctx, db.bob)
		if err != nil || n == 0 {
			return int(n), err
		}

		// Mark purged records as deleted.
		now := time.Now().Unix()
		_, err = models.Records.Update(
			um.SetCol("format").ToArg(nil),
			um.SetCol("value").ToArg(nil),
			um.SetCol("deleted").ToArg(now),
			models.UpdateWhere.Records.Key.Like(q.DatabaseKeyPrefix()+"%"),
		).Exec(db.ctx, db.bob)
		return int(n), err
	}

	// Otherwise, iterate over all entries and delete matching ones.

	// TODO: Non-local, non-internal or content matching queries are not supported at the moment.
	return 0, storage.ErrNotImplemented
}

// PurgeOlderThan deletes all records last updated before the given time. It returns the number of successful deletes and an error.
func (db *SQLite) PurgeOlderThan(ctx context.Context, prefix string, purgeBefore time.Time, local, internal, shadowDelete bool) (int, error) {
	db.wg.Add(1)
	defer db.wg.Done()

	purgeBeforeInt := purgeBefore.Unix()

	// Optimize for local and internal queries without where clause and without shadow delete.
	if local && internal && !shadowDelete {
		// First count entries (SQLite does not support affected rows)
		n, err := models.Records.Query(
			models.SelectWhere.Records.Key.Like(prefix+"%"),
			models.SelectWhere.Records.Modified.LT(purgeBeforeInt),
		).Count(db.ctx, db.bob)
		if err != nil || n == 0 {
			return int(n), err
		}

		// Delete entries.
		_, err = models.Records.Delete(
			models.DeleteWhere.Records.Key.Like(prefix+"%"),
			models.DeleteWhere.Records.Modified.LT(purgeBeforeInt),
		).Exec(db.ctx, db.bob)
		return int(n), err
	}

	// Optimize for local and internal queries without where clause, but with shadow delete.
	if local && internal && shadowDelete {
		// First count entries (SQLite does not support affected rows)
		n, err := models.Records.Query(
			models.SelectWhere.Records.Key.Like(prefix+"%"),
			models.SelectWhere.Records.Modified.LT(purgeBeforeInt),
		).Count(db.ctx, db.bob)
		if err != nil || n == 0 {
			return int(n), err
		}

		// Mark purged records as deleted.
		now := time.Now().Unix()
		_, err = models.Records.Update(
			um.SetCol("format").ToArg(nil),
			um.SetCol("value").ToArg(nil),
			um.SetCol("deleted").ToArg(now),
			models.UpdateWhere.Records.Key.Like(prefix+"%"),
			models.UpdateWhere.Records.Modified.LT(purgeBeforeInt),
		).Exec(db.ctx, db.bob)
		return int(n), err
	}

	// TODO: Non-local or non-internal queries are not supported at the moment.
	return 0, storage.ErrNotImplemented
}

// ReadOnly returns whether the database is read only.
func (db *SQLite) ReadOnly() bool {
	return false
}

// Injected returns whether the database is injected.
func (db *SQLite) Injected() bool {
	return false
}

// MaintainRecordStates maintains records states in the database.
func (db *SQLite) MaintainRecordStates(ctx context.Context, purgeDeletedBefore time.Time, shadowDelete bool) error {
	db.wg.Add(1)
	defer db.wg.Done()

	now := time.Now().Unix()
	purgeThreshold := purgeDeletedBefore.Unix()

	// Option 1: Using shadow delete.
	if shadowDelete {
		// Mark expired records as deleted.
		_, err := models.Records.Update(
			um.SetCol("format").ToArg(nil),
			um.SetCol("value").ToArg(nil),
			um.SetCol("deleted").ToArg(now),
			models.UpdateWhere.Records.Deleted.EQ(0),
			models.UpdateWhere.Records.Expires.GT(0),
			models.UpdateWhere.Records.Expires.LT(now),
		).Exec(db.ctx, db.bob)
		if err != nil {
			return fmt.Errorf("failed to shadow delete expired records: %w", err)
		}

		// Purge deleted records before threshold.
		_, err = models.Records.Delete(
			models.DeleteWhere.Records.Deleted.GT(0),
			models.DeleteWhere.Records.Deleted.LT(purgeThreshold),
		).Exec(db.ctx, db.bob)
		if err != nil {
			return fmt.Errorf("failed to purge deleted records (before threshold): %w", err)
		}
		return nil
	}

	// Option 2: Immediate delete.

	// Delete expired record.
	_, err := models.Records.Delete(
		models.DeleteWhere.Records.Expires.GT(0),
		models.DeleteWhere.Records.Expires.LT(now),
	).Exec(db.ctx, db.bob)
	if err != nil {
		return fmt.Errorf("failed to delete expired records: %w", err)
	}

	// Delete shadow deleted records.
	_, err = models.Records.Delete(
		models.DeleteWhere.Records.Deleted.GT(0),
	).Exec(db.ctx, db.bob)
	if err != nil {
		return fmt.Errorf("failed to purge deleted records: %w", err)
	}

	return nil
}

func (db *SQLite) Maintain(ctx context.Context) error {
	db.wg.Add(1)
	defer db.wg.Done()

	// Remove up to about 100KB of SQLite pages from the freelist on every run.
	// (Assuming 4KB page size.)
	_, err := db.db.ExecContext(ctx, "PRAGMA incremental_vacuum(25);")
	return err
}

func (db *SQLite) MaintainThorough(ctx context.Context) error {
	db.wg.Add(1)
	defer db.wg.Done()

	// Remove all pages from the freelist.
	_, err := db.db.ExecContext(ctx, "PRAGMA incremental_vacuum;")
	return err
}

// Shutdown shuts down the database.
func (db *SQLite) Shutdown() error {
	db.wg.Wait()
	db.cancelCtx()

	return db.bob.Close()
}

type statementLogger struct{}

func (sl statementLogger) Log(ctx context.Context, level sqldblogger.Level, msg string, data map[string]interface{}) {
	fmt.Printf("SQL: %s --- %+v\n", msg, data)
}
