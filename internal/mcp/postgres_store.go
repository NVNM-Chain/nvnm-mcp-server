// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// PostgresKeyStore is the multi-replica api-key backend. CRUD persists
// immediately; Lookup is a single indexed candidate query. It satisfies
// KeyStoreBackend.
type PostgresKeyStore struct {
	pool   *pgxpool.Pool
	hasher *auth.KeyHasher
	now    func() time.Time
}

// Compile-time assertion that PostgresKeyStore satisfies KeyStoreBackend.
var _ KeyStoreBackend = (*PostgresKeyStore)(nil)

// NewPostgresKeyStore returns a Postgres-backed store using pool and the
// versioned hasher (nil hasher => v0/plain-sha256 only).
func NewPostgresKeyStore(pool *pgxpool.Pool, hasher *auth.KeyHasher) *PostgresKeyStore {
	p := &PostgresKeyStore{pool: pool, hasher: hasher}
	p.now = time.Now
	return p
}

// digestBytes converts a 64-char hex digest (as produced by auth.KeyHasher)
// to the raw 32-byte value stored in the BYTEA column. The hasher always
// emits valid hex, so a decode error indicates a programmer mistake.
func digestBytes(hexDigest string) []byte {
	b, err := hex.DecodeString(hexDigest)
	if err != nil {
		panic(fmt.Sprintf("digestBytes: invalid hex from hasher: %v", err))
	}
	return b
}

// Lookup probes the candidate digests and returns the matching entry with a
// RejectReason. AND enabled is dropped so disabled rows surface as revoked.
// The raw key is hashed per candidate; no raw key is retained.
func (p *PostgresKeyStore) Lookup(ctx context.Context, rawKey string) (*KeyEntry, auth.RejectReason) {
	cands := p.hasher.Candidates(rawKey)
	raw := make([][]byte, len(cands))
	for i, c := range cands {
		raw[i] = digestBytes(c.Hash)
	}
	row := p.pool.QueryRow(ctx,
		`SELECT id, key_hash, hash_version, key_prefix, roles, enabled, created_at, expires_at
		   FROM api_keys WHERE key_hash = ANY($1)`, raw)
	e, err := scanEntry(row)
	if err != nil {
		return nil, auth.RejectNotFound // pgx.ErrNoRows or scan error
	}
	reason := classifyEntry(e, p.now())
	if reason == auth.RejectNone {
		p.maybeRehash(ctx, rawKey, e)
	}
	return e, reason
}

// maybeRehash persistently upgrades a matched row to the active pepper
// scheme when its stored digest is not the active one. Idempotent and
// best-effort: a failed upgrade does not fail the authentication (the
// key already matched), it just retries on the next lookup.
func (p *PostgresKeyStore) maybeRehash(ctx context.Context, rawKey string, e *KeyEntry) {
	activeHash, activeVer := p.hasher.HashForStore(rawKey)
	if e.KeyHash == activeHash && e.HashVersion == activeVer {
		return // already at the active scheme
	}
	_, _ = p.pool.Exec(ctx, //nolint:errcheck // best-effort: a failed upgrade does not fail auth
		`UPDATE api_keys SET key_hash=$1, hash_version=$2 WHERE id=$3`,
		digestBytes(activeHash), activeVer, e.ID)
	// Reflect the upgrade in the returned entry for consistency.
	e.KeyHash, e.HashVersion = activeHash, activeVer
}

func scanEntry(row pgx.Row) (*KeyEntry, error) {
	var (
		e       KeyEntry
		hashB   []byte
		created time.Time
		expires *time.Time
	)
	if err := row.Scan(&e.ID, &hashB, &e.HashVersion, &e.KeyPrefix, &e.Roles, &e.Enabled, &created, &expires); err != nil {
		return nil, err
	}
	e.KeyHash = hex.EncodeToString(hashB)
	e.CreatedAt = created
	if expires != nil {
		e.ExpiresAt = *expires
	}
	return &e, nil
}

// Create generates a new key with optional expiry, persists it (hashed under
// the active scheme), and returns the raw key once. ErrClientExists if id taken.
func (p *PostgresKeyStore) Create(
	ctx context.Context, clientID string, roles []string, expiresAt time.Time,
) (*KeyCreateResult, error) {
	rawKey, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	hash, version := p.hasher.HashForStore(rawKey)
	entry := KeyEntry{
		ID:          clientID,
		KeyHash:     hash,
		HashVersion: version,
		KeyPrefix:   keyPrefixOf(rawKey),
		Enabled:     true,
		CreatedAt:   p.now().UTC(),
		Roles:       roles,
		ExpiresAt:   expiresAt,
	}
	var exp any
	if !expiresAt.IsZero() {
		exp = expiresAt
	}
	_, err = p.pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, hash_version, key_prefix, roles, enabled, created_at, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		entry.ID, digestBytes(hash), version, entry.KeyPrefix, roles, true, entry.CreatedAt, exp)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("client %q: %w", clientID, ErrClientExists)
		}
		return nil, fmt.Errorf("insert key: %w", err)
	}
	return &KeyCreateResult{KeySummary: summarize(&entry), Key: rawKey}, nil
}

// Update sets enabled and/or roles. ErrClientMissing if id absent.
func (p *PostgresKeyStore) Update(clientID string, upd KeyUpdate) (*KeySummary, error) {
	ctx := context.Background()
	set := ""
	args := []any{}
	i := 1
	if upd.Enabled != nil {
		set += fmt.Sprintf("enabled=$%d,", i)
		args = append(args, *upd.Enabled)
		i++
	}
	if upd.Roles != nil {
		set += fmt.Sprintf("roles=$%d,", i)
		args = append(args, *upd.Roles)
		i++
	}
	if upd.ExpiresAt != nil {
		set += fmt.Sprintf("expires_at=$%d,", i)
		if upd.ExpiresAt.IsZero() {
			args = append(args, nil)
		} else {
			args = append(args, *upd.ExpiresAt)
		}
		i++
	}
	if set == "" {
		return p.summary(ctx, clientID) // nothing to change
	}
	set = set[:len(set)-1] // trim trailing comma
	args = append(args, clientID)
	tag, err := p.pool.Exec(ctx,
		fmt.Sprintf("UPDATE api_keys SET %s WHERE id=$%d", set, i), args...)
	if err != nil {
		return nil, fmt.Errorf("update key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("client %q: %w", clientID, ErrClientMissing)
	}
	return p.summary(ctx, clientID)
}

// Delete removes a key. ErrClientMissing if absent.
func (p *PostgresKeyStore) Delete(clientID string) error {
	tag, err := p.pool.Exec(context.Background(), "DELETE FROM api_keys WHERE id=$1", clientID)
	if err != nil {
		return fmt.Errorf("delete key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("client %q: %w", clientID, ErrClientMissing)
	}
	return nil
}

func (p *PostgresKeyStore) summary(ctx context.Context, clientID string) (*KeySummary, error) {
	var (
		s       KeySummary
		created time.Time
		expires *time.Time
	)
	err := p.pool.QueryRow(ctx,
		`SELECT id, enabled, created_at, roles, key_prefix, expires_at
		   FROM api_keys WHERE id=$1`, clientID).
		Scan(&s.ID, &s.Enabled, &created, &s.Roles, &s.KeyPrefix, &expires)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("client %q: %w", clientID, ErrClientMissing)
		}
		return nil, fmt.Errorf("fetch summary %q: %w", clientID, err)
	}
	s.CreatedAt = created
	if expires != nil {
		s.ExpiresAt = *expires
	}
	return &s, nil
}

// List returns redacted summaries of all keys.
func (p *PostgresKeyStore) List() []KeySummary {
	rows, err := p.pool.Query(context.Background(),
		`SELECT id, enabled, created_at, roles, key_prefix, expires_at
		   FROM api_keys ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []KeySummary
	for rows.Next() {
		var (
			s       KeySummary
			created time.Time
			expires *time.Time
		)
		if err := rows.Scan(&s.ID, &s.Enabled, &created, &s.Roles, &s.KeyPrefix, &expires); err != nil {
			return out
		}
		s.CreatedAt = created
		if expires != nil {
			s.ExpiresAt = *expires
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

// countAll returns the total number of keys.
func (p *PostgresKeyStore) countAll() int {
	var n int
	if err := p.pool.QueryRow(context.Background(), "SELECT count(*) FROM api_keys").Scan(&n); err != nil {
		return 0
	}
	return n
}

// countEnabled returns the number of enabled keys.
func (p *PostgresKeyStore) countEnabled() int {
	var n int
	if err := p.pool.QueryRow(context.Background(), "SELECT count(*) FROM api_keys WHERE enabled").Scan(&n); err != nil {
		return 0
	}
	return n
}

// Empty reports whether there are no enabled keys.
func (p *PostgresKeyStore) Empty() bool { return p.countEnabled() == 0 }

// ActiveCount returns the number of enabled keys.
func (p *PostgresKeyStore) ActiveCount() int { return p.countEnabled() }

// TotalCount returns the total number of keys.
func (p *PostgresKeyStore) TotalCount() int { return p.countAll() }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
