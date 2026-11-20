package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	migrate "github.com/rubenv/sql-migrate"
	sqldblogger "github.com/simukti/sqldb-logger"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/sqlite/dialect"
	"github.com/stephenafamo/bob/dialect/sqlite/sm"
	"github.com/stephenafamo/bob/dialect/sqlite/um"
	_ "modernc.org/sqlite"

	"github.com/safing/portmaster/base/database/accessor"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/database/storage/sqlite/models"
	"github.com/safing/portmaster/base/log"
)

// Errors.
var (
	ErrQueryTimeout = errors.New("query timeout")

	errShuttingDown = errors.New("database is shutting down")
)

// queryPageSize is the number of rows a query loads from the database at once.
const queryPageSize = 100

// SQLite storage.
type SQLite struct {
	name string

	db  *sql.DB
	bob bob.DB
	wg  sync.WaitGroup
	// batchWg tracks the handlers of PutMany batches, which keep running
	// after PutMany returned. They must not use wg, as Shutdown may already
	// wait on it.
	batchWg sync.WaitGroup

	// singleUpsert writes one record, see buildUpsert.
	singleUpsert string

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
	// _txlock = deferred

	// _time_format=sqlite stores timestamps as "YYYY-MM-DD HH:MM:SS.SSS±HH:MM", avoiding timezone abbreviations that break round-trip parsing (e.g. "UTC+2").
	dbFileDSN := dbFile + "?_time_format=sqlite"

	db, err := sql.Open("sqlite", dbFileDSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable statement printing.
	if printStmts {
		db = sqldblogger.OpenDriver(dbFileDSN, db.Driver(), &statementLogger{})
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

	// Limit concurrent writes to avoid "SQLITE_BUSY" errors on large workloads.
	// SQLite typically allows only one writer at a time anyway.
	// In WAL mode, concurrent readers can still function,
	// but we keep a single writer to reduce lock contention.
	db.SetMaxOpenConns(1)    // Only 1 open connection can be active
	db.SetMaxIdleConns(1)    // Maintain at most 1 idle connection in the pool
	db.SetConnMaxLifetime(0) // Keep the single connection alive indefinitely

	// Run migrations on database.
	n, err := migrate.Exec(db, "sqlite3", getMigrations(), migrate.Up)
	if err != nil {
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	log.Debugf("database/sqlite: ran %d migrations on %s database", n, name)

	// Return as bob database.
	ctx, cancelCtx := context.WithCancel(context.Background())
	sqliteDB := &SQLite{
		name:      name,
		db:        db,
		bob:       bob.NewDB(db),
		ctx:       ctx,
		cancelCtx: cancelCtx,
	}
	sqliteDB.singleUpsert, err = sqliteDB.buildUpsert(1)
	if err != nil {
		cancelCtx()
		_ = db.Close()
		return nil, err
	}
	return sqliteDB, nil
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
	db.wg.Add(1)
	defer db.wg.Done()

	return db.putRecord(r, nil)
}

// putRecord writes the record. It does not register as a database operation
// that Shutdown waits for, as it is also used by the batch handlers.
func (db *SQLite) putRecord(r record.Record, tx *bob.Tx) (record.Record, error) {
	// Lock record if in a transaction.
	if tx != nil {
		r.Lock()
		defer r.Unlock()
	}

	args, err := recordArgs(r)
	if err != nil {
		return nil, err
	}

	// Execute in transaction or directly.
	if tx != nil {
		_, err = tx.ExecContext(db.ctx, db.singleUpsert, args...)
	} else {
		_, err = db.bob.ExecContext(db.ctx, db.singleUpsert, args...)
	}
	if err != nil {
		return nil, err
	}

	return r, nil
}

// PutMany stores many records in the database.
func (db *SQLite) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	return db.PutManyAbortable(context.Background(), shadowDelete)
}

// PutManyAbortable stores many records in the database like PutMany, unless
// abort is done before the batch channel is closed: the transaction of the
// batch is then rolled back and the result wraps storage.ErrBatchAborted.
func (db *SQLite) PutManyAbortable(abort context.Context, shadowDelete bool) (chan<- record.Record, <-chan error) {
	db.wg.Add(1)
	defer db.wg.Done()

	// Check if we should use prepared statement optimized inserting.
	if UsePreparedStatements {
		return db.putManyWithPreparedStmts(abort)
	}

	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)

	tx, err := db.bob.BeginTx(db.ctx, nil)
	if err != nil {
		errs <- err
		return batch, errs
	}

	// start handler
	db.batchWg.Add(1)
	go func() {
		defer db.batchWg.Done()
		var err error

		// Read all put records.
	writeBatch:
		for {
			select {
			case r := <-batch:
				if r != nil {
					// Write record.
					_, err = db.putRecord(r, &tx)
					if err != nil {
						break writeBatch
					}
				} else {
					// Also check for an abort when the batch channel was
					// closed, as it is closed right after the abort.
					if abort.Err() != nil {
						err = batchAbortedErr(abort)
						break writeBatch
					}

					// Finalize transcation.
					errs <- tx.Commit(db.ctx)
					return
				}

			case <-abort.Done():
				err = batchAbortedErr(abort)
				break writeBatch

			case <-db.ctx.Done():
				err = db.shuttingDownErr()
				break writeBatch
			}
		}

		// Rollback transaction and report why the batch was aborted.
		// Exactly one value must be sent on errs.
		db.rollbackBatch(tx)
		errs <- err
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

	var (
		err       error
		lastKey   string
		firstPage = true
		page      models.RecordSlice
	)

	// Load the records page by page (keyset pagination over the primary key).
	// Each page is fully loaded before it is handed to the consumer, so that
	// the single database connection is not held while waiting for the
	// consumer. Otherwise, all other database operations - including any the
	// consumer itself does while iterating - would block until the query ends.
pagesLoop:
	for {
		mods := []bob.Mod[*dialect.SelectQuery]{
			sm.OrderBy(models.Records.Columns.Key).Asc(),
			sm.Limit(queryPageSize),
		}
		if q.DatabaseKeyPrefix() != "" {
			mods = append(mods, models.SelectWhere.Records.Key.Like(q.DatabaseKeyPrefix()+"%"))
		}
		if !firstPage {
			mods = append(mods, models.SelectWhere.Records.Key.GT(lastKey))
		}
		firstPage = false
		page, err = models.Records.View.Query(mods...).All(db.ctx, db.bob)
		if err != nil {
			err = fmt.Errorf("failed to load records: %w", err)
			break pagesLoop
		}
		if len(page) == 0 {
			break pagesLoop
		}
		lastKey = page[len(page)-1].Key

		var done bool
		done, err = db.feedQueryPage(queryIter, q, page, local, internal)
		if done || err != nil || len(page) < queryPageSize {
			break pagesLoop
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
	// Wait for the batch handlers, which abort their batch when the context
	// is canceled, before closing the database they use.
	db.batchWg.Wait()

	return db.bob.Close()
}

// shuttingDownErr returns the error for a batch that was aborted because the
// database is shutting down.
func (db *SQLite) shuttingDownErr() error {
	return fmt.Errorf("%w: %w", errShuttingDown, db.ctx.Err())
}

// batchAbortedErr returns the error for a batch that was aborted by its
// caller.
func batchAbortedErr(abort context.Context) error {
	return fmt.Errorf("%w: %w", storage.ErrBatchAborted, context.Cause(abort))
}

// rollbackBatch rolls back the transaction of an aborted batch.
func (db *SQLite) rollbackBatch(tx bob.Tx) {
	// The transaction is already rolled back when the context was canceled.
	err := tx.Rollback(db.ctx)
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Warningf("database/sqlite: failed to roll back aborted batch of %s database: %s", db.name, err)
	}
}

// feedQueryPage sends the matching records of the given page to the query
// iterator. It returns whether the consumer is done or an error.
func (db *SQLite) feedQueryPage(queryIter *iterator.Iterator, q *query.Query, page models.RecordSlice, local, internal bool) (done bool, err error) {
	for _, r := range page {
		// Check if key matches.
		if !q.MatchesKey(r.Key) {
			continue
		}

		// Check Meta.
		m := getMeta(r)
		if !m.CheckValidity() ||
			!m.CheckPermission(local, internal) {
			continue
		}

		// Check Data.
		if q.HasWhereCondition() {
			if r.Format.IsNull() || r.Value.IsNull() {
				continue
			}

			jsonData := string(r.Value.GetOrZero())
			jsonAccess := accessor.NewJSONAccessor(&jsonData)
			if !q.MatchesAccessor(jsonAccess) {
				continue
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
			return true, nil
		case queryIter.Next <- matched:
		default:
			select {
			case <-queryIter.Done:
				return true, nil
			case queryIter.Next <- matched:
			case <-time.After(1 * time.Second):
				return true, ErrQueryTimeout
			}
		}
	}

	return false, nil
}

type statementLogger struct{}

func (sl statementLogger) Log(ctx context.Context, level sqldblogger.Level, msg string, data map[string]interface{}) {
	fmt.Printf("SQL: %s --- %+v\n", msg, data)
}
