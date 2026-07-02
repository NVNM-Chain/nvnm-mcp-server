-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 Inveniam Capital Partners

-- +goose Up
CREATE TABLE signer_quota (
    signer       TEXT        NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    count        INTEGER     NOT NULL DEFAULT 0,
    PRIMARY KEY (signer, window_start)
);

CREATE TABLE signer_blacklist (
    signer     TEXT        PRIMARY KEY,
    reason     TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE signer_blacklist;
DROP TABLE signer_quota;
