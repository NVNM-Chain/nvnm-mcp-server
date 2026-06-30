-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 Inveniam Capital Partners
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS write_audit (
    id            BIGSERIAL   PRIMARY KEY,
    signer        TEXT        NOT NULL,
    to_addr       TEXT        NOT NULL DEFAULT '',
    value_wei     TEXT        NOT NULL DEFAULT '0',
    calldata_len  INT         NOT NULL DEFAULT 0,
    tx_hash       TEXT        NOT NULL DEFAULT '',
    outcome       TEXT        NOT NULL,
    error         TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS write_audit_signer_created_idx ON write_audit (signer, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS write_audit;
-- +goose StatementEnd
