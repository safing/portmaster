package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/sqlite"
	"github.com/stephenafamo/bob/dialect/sqlite/im"
	"github.com/stephenafamo/bob/expr"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/structures/dsd"
)

var UsePreparedStatements bool = true

// upsertRowsPerStatement is the number of records written with one INSERT.
// The driver parses the SQL on every execution, which costs about as much
// as the insert itself, so writing several rows per statement roughly
// halves the time per record. Much larger statements get slower again.
const upsertRowsPerStatement = 50

// recordColumns are the columns written by the upsert, in argument order.
var recordColumns = []string{"key", "format", "value", "created", "modified", "expires", "deleted", "secret", "crownjewel"}

// buildUpsert builds an INSERT for the given number of rows that replaces
// existing records with the same key.
//
// The statement is built with the plain SQLite dialect, not with the
// generated table model: the model adds a RETURNING clause with every
// column, so SQLite would produce a result row with the full record value
// for each write, only for the driver to discard it.
func (db *SQLite) buildUpsert(rows int) (string, error) {
	valueRows := make([][]bob.Expression, rows)
	for i := range valueRows {
		row := make([]bob.Expression, len(recordColumns))
		for j, column := range recordColumns {
			row[j] = expr.Arg(column)
		}
		valueRows[i] = row
	}

	// Simulate upsert with custom selection on conflict.
	rawQuery, _, err := sqlite.Insert(
		im.Into("records", recordColumns...),
		im.Rows(valueRows...),
		im.OnConflict("key").DoUpdate(
			im.SetExcluded(recordColumns[1:]...),
		),
	).Build(db.ctx)
	if err != nil {
		return "", fmt.Errorf("build upsert for %d rows: %w", rows, err)
	}
	return anonymizePlaceholders(rawQuery, rows*len(recordColumns))
}

var errUnexpectedPlaceholders = errors.New("unexpected placeholders in upsert")

// anonymizePlaceholders replaces the numbered placeholders ?1, ?2, ... ?n in
// query with anonymous ? placeholders. SQLite numbers anonymous placeholders
// in the order they appear, so this is only done if the numbered placeholders
// appear exactly in the order 1..n; otherwise an error is returned.
//
// The modernc.org/sqlite driver matches every numbered placeholder against
// all arguments by formatting each argument's ordinal as a string, which
// makes binding the arguments of a multi-row insert quadratic and allocate
// for every comparison. Anonymous placeholders are matched by ordinal.
func anonymizePlaceholders(query string, n int) (string, error) {
	var b strings.Builder
	b.Grow(len(query))

	next := 1
	for i := 0; i < len(query); i++ {
		c := query[i]
		b.WriteByte(c)
		if c != '?' {
			continue
		}

		// Read the placeholder number.
		end := i + 1
		for end < len(query) && query[end] >= '0' && query[end] <= '9' {
			end++
		}
		num, err := strconv.Atoi(query[i+1 : end])
		if err != nil || num != next {
			return "", fmt.Errorf("%w: found %q, expected ?%d", errUnexpectedPlaceholders, query[i:end], next)
		}
		next++
		i = end - 1
	}

	if next-1 != n {
		return "", fmt.Errorf("%w: found %d, expected %d", errUnexpectedPlaceholders, next-1, n)
	}
	return b.String(), nil
}

// putManyWithPreparedStmts stores many records in the database. The batch is
// rolled back if abort is done before the batch channel is closed.
func (db *SQLite) putManyWithPreparedStmts(abort context.Context) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)

	fullUpsert, err := db.buildUpsert(upsertRowsPerStatement)
	if err != nil {
		errs <- err
		return batch, errs
	}

	// Start transaction.
	tx, err := db.bob.BeginTx(db.ctx, nil)
	if err != nil {
		errs <- err
		return batch, errs
	}

	// Create prepared statement WITHIN TRANSACTION.
	preparedStmt, err := tx.PrepareContext(db.ctx, fullUpsert)
	if err != nil {
		_ = tx.Rollback(db.ctx)
		errs <- err
		return batch, errs
	}

	// start handler
	db.batchWg.Add(1)
	go func() {
		defer db.batchWg.Done()
		// Arguments of the records collected for the next statement.
		args := make([]any, 0, upsertRowsPerStatement*len(recordColumns))
		pending := 0
		var err error

		// Read all put records.
	writeBatch:
		for {
			select {
			case r := <-batch:
				if r != nil {
					// Collect record and write when a full statement is reached.
					var recordArgs []any
					recordArgs, err = upsertArgs(r)
					if err != nil {
						break writeBatch
					}
					args = append(args, recordArgs...)
					pending++
					if pending < upsertRowsPerStatement {
						continue
					}

					_, err = preparedStmt.ExecContext(db.ctx, args...)
					if err != nil {
						break writeBatch
					}
					args = args[:0]
					pending = 0
				} else {
					// Also check for an abort when the batch channel was
					// closed, as it is closed right after the abort.
					if abort.Err() != nil {
						err = batchAbortedErr(abort)
						break writeBatch
					}

					// Write the remaining records with a statement of matching size.
					if pending > 0 {
						var remainderUpsert string
						remainderUpsert, err = db.buildUpsert(pending)
						if err != nil {
							break writeBatch
						}
						_, err = tx.ExecContext(db.ctx, remainderUpsert, args...)
						if err != nil {
							break writeBatch
						}
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

// upsertArgs returns the statement arguments of r in recordColumns order.
func upsertArgs(r record.Record) ([]any, error) {
	r.Lock()
	defer r.Unlock()

	return recordArgs(r)
}

// recordArgs returns the statement arguments of r in recordColumns order.
// The caller must hold the lock of r.
func recordArgs(r record.Record) ([]any, error) {
	// default serialization format - JSON
	format := uint8(dsd.JSON)

	// For wrapped records, check the required format
	if r.IsWrapped() {
		wrapper, ok := r.(*record.Wrapper)
		if !ok {
			return nil, fmt.Errorf("%w: reports to be wrapped but is not of type *record.Wrapper", storage.ErrRecordMalformed)
		}
		format, ok = dsd.ValidateSerializationFormat(wrapper.Format)
		if !ok {
			return nil, dsd.ErrIncompatibleFormat
		}
	}

	// Serialize.
	data, err := r.MarshalDataOnly(r, format)
	if err != nil {
		return nil, err
	}

	// Records without data are stored with NULL format and value.
	var formatArg, valueArg any
	if len(data) > 0 {
		formatArg = int64(format)
		valueArg = data
	}

	m := r.Meta()
	return []any{
		r.DatabaseKey(),
		formatArg,
		valueArg,
		m.Created,
		m.Modified,
		m.Expires,
		m.Deleted,
		m.IsSecret(),
		m.IsCrownJewel(),
	}, nil
}
