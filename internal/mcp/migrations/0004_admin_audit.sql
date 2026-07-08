-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 Inveniam Capital Partners

-- +goose Up
CREATE TABLE IF NOT EXISTS admin_audit (
    id         BIGSERIAL   PRIMARY KEY,
    actor_id   TEXT        NOT NULL,
    action     TEXT        NOT NULL,
    target     TEXT        NOT NULL DEFAULT '',
    detail     TEXT        NOT NULL DEFAULT '',
    outcome    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS admin_audit_actor_created_idx ON admin_audit (actor_id, created_at);
CREATE INDEX IF NOT EXISTS admin_audit_action_created_idx ON admin_audit (action, created_at);

-- +goose Down
DROP TABLE IF EXISTS admin_audit;
