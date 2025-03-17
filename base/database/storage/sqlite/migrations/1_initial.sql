-- +migrate Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE records (
    key TEXT PRIMARY KEY,

    format SMALLINT,
    value  BLOB,

    created    BIGINT NOT NULL,
    modified   BIGINT NOT NULL,
    expires    BIGINT DEFAULT 0 NOT NULL,
    deleted    BIGINT DEFAULT 0 NOT NULL,
    secret     BOOLEAN DEFAULT FALSE NOT NULL,
    crownjewel BOOLEAN DEFAULT FALSE NOT NULL
);

-- +migrate Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE records;
