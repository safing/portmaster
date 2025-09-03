package sqlite

import (
	"context"
	"fmt"
	"strconv"

	"github.com/safing/structures/dsd"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/sqlite/im"
	"github.com/stephenafamo/bob/expr"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/database/storage/sqlite/models"
)

var UsePreparedStatements bool = true

// PutMany stores many records in the database.
func (db *SQLite) putManyWithPreparedStmts(shadowDelete bool) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)

	// Simulate upsert with custom selection on conflict.
	rawQuery, _, err := models.Records.Insert(
		im.Into("records", "key", "format", "value", "created", "modified", "expires", "deleted", "secret", "crownjewel"),
		im.Values(expr.Arg("key"), expr.Arg("format"), expr.Arg("value"), expr.Arg("created"), expr.Arg("modified"), expr.Arg("expires"), expr.Arg("deleted"), expr.Arg("secret"), expr.Arg("crownjewel")),
		im.OnConflict("key").DoUpdate(
			im.SetExcluded("format", "value", "created", "modified", "expires", "deleted", "secret", "crownjewel"),
		),
	).Build(db.ctx)
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
	preparedStmt, err := tx.PrepareContext(db.ctx, rawQuery)
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
					err := writeWithPreparedStatement(db.ctx, &preparedStmt, r)
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

func writeWithPreparedStatement(ctx context.Context, pStmt *bob.StdPrepared, r record.Record) error {
	r.Lock()
	defer r.Unlock()

	// default serialization format - JSON
	format := uint8(dsd.JSON)

	// For wrapped records, check the required format
	if r.IsWrapped() {
		wrapper, ok := r.(*record.Wrapper)
		if !ok {
			return fmt.Errorf("%w: reports to be wrapped but is not of type *record.Wrapper", storage.ErrRecordMalformed)
		}
		format, ok = dsd.ValidateSerializationFormat(wrapper.Format)
		if !ok {
			return dsd.ErrIncompatibleFormat
		}
	}

	// Serialize.
	data, err := r.MarshalDataOnly(r, format)
	if err != nil {
		return err
	}

	// Get Meta.
	m := r.Meta()

	// Insert.
	if len(data) > 0 {
		format := strconv.Itoa(int(format))
		_, err = pStmt.ExecContext(
			ctx,
			r.DatabaseKey(),
			format,
			data,
			m.Created,
			m.Modified,
			m.Expires,
			m.Deleted,
			m.IsSecret(),
			m.IsCrownJewel(),
		)
	} else {
		_, err = pStmt.ExecContext(
			ctx,
			r.DatabaseKey(),
			nil,
			nil,
			m.Created,
			m.Modified,
			m.Expires,
			m.Deleted,
			m.IsSecret(),
			m.IsCrownJewel(),
		)
	}
	return err
}
