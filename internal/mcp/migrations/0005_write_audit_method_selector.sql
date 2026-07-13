-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 Inveniam Capital Partners

-- Retention (Privacy Policy § 8) gives grantRole broadcasts a longer window
-- than ordinary anchor writes. Under keyless writes every relayed tx shares
-- one destination (checkRelayScope permits only the anchor precompile), so
-- to_addr cannot tell the two apart, and calldata_len is not authoritative.
-- Persist the 4-byte ABI method selector so the purge job can apply the two
-- windows the policy actually promises. A selector is a public function
-- identifier, not caller data -- it carries no personal information.
--
-- Rows written before this migration have method_selector = '' (unknown).
-- purgeWriteAudit treats '' as "not grantRole" and applies the ordinary
-- window; see the retentionUnknownSelector note in purge.go.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE write_audit
    ADD COLUMN IF NOT EXISTS method_selector TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
-- The purge deletes by age; without this the DELETE degrades to a seq scan
-- once the table is large, which is exactly when the purge matters most.
CREATE INDEX IF NOT EXISTS write_audit_created_at_idx ON write_audit (created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS write_audit_created_at_idx;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE write_audit DROP COLUMN IF EXISTS method_selector;
-- +goose StatementEnd
