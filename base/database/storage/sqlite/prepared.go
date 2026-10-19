package sqlite

import (
	"fmt"

	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/sqlite/im"
	"github.com/stephenafamo/bob/expr"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/database/storage/sqlite/models"
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
	rawQuery, _, err := models.Records.Insert(
		im.Into("records", recordColumns...),
		im.Rows(valueRows...),
		im.OnConflict("key").DoUpdate(
			im.SetExcluded(recordColumns[1:]...),
		),
	).Build(db.ctx)
	if err != nil {
		return "", fmt.Errorf("build upsert for %d rows: %w", rows, err)
	}
	return rawQuery, nil
}

// PutMany stores many records in the database.
func (db *SQLite) putManyWithPreparedStmts(shadowDelete bool) (chan<- record.Record, <-chan error) {
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
	go func() {
		// Arguments of the records collected for the next statement.
		args := make([]any, 0, upsertRowsPerStatement*len(recordColumns))
		pending := 0

		// Read all put records.
	writeBatch:
		for {
			select {
			case r := <-batch:
				if r != nil {
					// Collect record and write when a full statement is reached.
					recordArgs, err := upsertArgs(r)
					if err != nil {
						errs <- err
						break writeBatch
					}
					args = append(args, recordArgs...)
					pending++
					if pending < upsertRowsPerStatement {
						continue
					}

					_, err = preparedStmt.ExecContext(db.ctx, args...)
					if err != nil {
						errs <- err
						break writeBatch
					}
					args = args[:0]
					pending = 0
				} else {
					// Write the remaining records with a statement of matching size.
					if pending > 0 {
						remainderUpsert, err := db.buildUpsert(pending)
						if err != nil {
							errs <- err
							break writeBatch
						}
						_, err = tx.ExecContext(db.ctx, remainderUpsert, args...)
						if err != nil {
							errs <- err
							break writeBatch
						}
					}

					// Finalize transcation.
					errs <- tx.Commit(db.ctx)
					return
				}

			case <-db.ctx.Done():
				break writeBatch
			}
		}

		// Rollback transaction.
		errs <- tx.Rollback(db.ctx)
	}()

	return batch, errs
}

// upsertArgs returns the statement arguments of r in recordColumns order.
func upsertArgs(r record.Record) ([]any, error) {
	r.Lock()
	defer r.Unlock()

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
