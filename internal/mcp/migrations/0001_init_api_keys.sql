-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS api_keys (
    id            TEXT        PRIMARY KEY,
    key_hash      BYTEA       NOT NULL,
    hash_version  INT         NOT NULL DEFAULT 0,
    key_prefix    TEXT        NOT NULL DEFAULT '',
    roles         TEXT[]      NOT NULL DEFAULT '{}',
    enabled       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS api_keys_key_hash_idx ON api_keys (key_hash);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS api_keys;
-- +goose StatementEnd
