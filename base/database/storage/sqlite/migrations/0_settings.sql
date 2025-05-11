-- +migrate Up
-- SQL in section 'Up' is executed when this migration is applied
PRAGMA auto_vacuum = INCREMENTAL; -- https://sqlite.org/pragma.html#pragma_auto_vacuum

-- +migrate Down
-- SQL section 'Down' is executed when this migration is rolled back
PRAGMA auto_vacuum = NONE; -- https://sqlite.org/pragma.html#pragma_auto_vacuum
