// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Pending request statuses. Constants rather than an enum so the JSON
// shape is plain string, and so Prometheus labels (mcp_key_requests_total
// per the Phase 9.8 task list) bucket cleanly on the same values.
const (
	PendingStatusPending  = "pending"
	PendingStatusApproved = "approved"
	PendingStatusRejected = "rejected"
)

// ErrPendingNotFound is returned when a lookup or decision targets an ID
// that is not in the store. Callers can distinguish via errors.Is.
var ErrPendingNotFound = errors.New("pending key request not found")

// ErrPendingNotPending is returned when an admin attempts to decide a
// request that has already been decided. Prevents a double-approve race
// where two admins click approve at nearly the same time and the second
// one re-emails the customer.
var ErrPendingNotPending = errors.New("pending key request is not in 'pending' status")

// ErrPendingEmptyPath is returned by NewPendingKeyStore when the
// caller passes an empty path; callers can distinguish from generic
// file-system errors via errors.Is.
var ErrPendingEmptyPath = errors.New("pending key store: empty path")

// ErrPendingInvalidStatus is returned by Decide when the supplied
// status is not one of PendingStatusApproved or PendingStatusRejected.
// Pending is excluded because a request is already in that status when
// Decide is called.
var ErrPendingInvalidStatus = errors.New("invalid decision status")

// PendingKeyRequest is one self-serve API-key request submitted via
// POST /api/v1/keys/request and awaiting human triage. The customer-
// supplied fields (Email / Company / IntendedUse) match the Phase 11
// RD1 PII schema; everything else is server-generated audit metadata.
//
// JSON field names are snake_case to match the existing KeyEntry shape
// and the Privacy Policy's data-handling table; the on-disk store is
// not user-facing but the JSON shape leaks into admin tooling that
// inspects keys_pending.json directly.
type PendingKeyRequest struct {
	// ID is a UUID generated server-side. Stable for the lifetime of
	// the request; surfaced to the caller in the 202 response and used
	// as the admin-route path parameter.
	ID string `json:"id"`

	// Email is required. Validated as a syntactically plausible address
	// by the public endpoint before reaching this store; the store
	// itself accepts any string.
	Email string `json:"email"`

	// Company is optional (per RD1). Empty string is valid.
	Company string `json:"company,omitempty"`

	// IntendedUse is the customer's free-text answer to "what are you
	// building?" — load-bearing for reviewer judgement; body-size
	// limit is enforced by the public endpoint.
	IntendedUse string `json:"intended_use"`

	// Status is one of PendingStatus* values. Mutated only via Decide.
	Status string `json:"status"`

	// CreatedAt is the RFC 3339 UTC timestamp of the request.
	CreatedAt time.Time `json:"created_at"`

	// DecidedAt is set when Status transitions to approved or rejected.
	DecidedAt *time.Time `json:"decided_at,omitempty"`

	// DeciderID is the admin's auth client_id; populated by Decide.
	// Audit trail for who approved/rejected.
	DeciderID string `json:"decider_id,omitempty"`

	// KeyID is the issued KeyEntry.ID after an approve decision. Empty
	// on pending or rejected requests. Lets admin tools cross-reference
	// from a pending request to the live credential it produced.
	KeyID string `json:"key_id,omitempty"`

	// RemoteAddr is the source IP observed by the public endpoint; for
	// abuse audit only. Stored deliberately per Phase 11 § 5 D-L3-3
	// (spam/flood threat model).
	RemoteAddr string `json:"remote_addr,omitempty"`
}

// PendingKeyStore is a file-backed JSON store of PendingKeyRequest with
// atomic writes and an advisory exclusive lock during write. Concurrency
// model mirrors KeyStore in keys.go: an in-memory copy guards reads
// behind a sync.RWMutex, every mutation re-serializes the full slice to
// disk via tmp-file + fsync + rename, and writes hold a file-level
// LOCK_EX so cooperating processes don't clobber each other.
//
// Trade-off: full re-serialize on every mutation is O(N) in the request
// count. Acceptable for the closed-beta volume RD3 documents (2–4 weeks,
// hand-picked customers, 10–20 cohort). Revisit when volume justifies
// per-record patching or a real database.
type PendingKeyStore struct {
	mu    sync.RWMutex
	path  string
	items []PendingKeyRequest
}

// NewPendingKeyStore opens the file at path and loads any existing
// requests. A missing file is treated as an empty store — the first
// mutation creates it. An unparseable file is a hard error: a corrupt
// pending store is operator-resolvable but should not be silently
// truncated.
func NewPendingKeyStore(path string) (*PendingKeyStore, error) {
	if path == "" {
		return nil, ErrPendingEmptyPath
	}
	s := &PendingKeyStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PendingKeyStore) load() error {
	// path is operator-supplied config; not user input.
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.items = nil
			return nil
		}
		return fmt.Errorf("read pending keys file: %w", err)
	}
	if len(data) == 0 {
		s.items = nil
		return nil
	}
	var entries []PendingKeyRequest
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse pending keys file: %w", err)
	}
	s.items = entries
	return nil
}

// Add appends a new pending request and persists. The caller supplies
// Email / Company / IntendedUse / RemoteAddr; ID / Status / CreatedAt
// are server-generated. Returns the populated request so the caller
// can echo the ID back in the 202 response.
//
// Serialization note: the write lock is held across the on-disk save so
// concurrent in-process Adds do not race on the LOCK_EX|LOCK_NB flock
// inside save (which would manifest as "resource temporarily
// unavailable" failures for the loser). RWMutex readers still see a
// consistent snapshot. The save window is short (kilobytes, fsync) at
// the closed-beta volume RD3 targets.
func (s *PendingKeyStore) Add(email, company, intendedUse, remoteAddr string) (PendingKeyRequest, error) {
	req := PendingKeyRequest{
		ID:          uuid.NewString(),
		Email:       email,
		Company:     company,
		IntendedUse: intendedUse,
		Status:      PendingStatusPending,
		CreatedAt:   time.Now().UTC(),
		RemoteAddr:  remoteAddr,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = append(s.items, req)
	if err := s.save(s.items); err != nil {
		// Roll back the in-memory append so the store stays consistent
		// with disk. Otherwise a subsequent successful Add would persist
		// the failed request too.
		s.items = s.items[:len(s.items)-1]
		return PendingKeyRequest{}, err
	}
	return req, nil
}

// Get returns the request with the given ID and whether it was found.
// Returns a value copy; the caller cannot mutate store state through
// the returned struct.
func (s *PendingKeyStore) Get(id string) (PendingKeyRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.items {
		if s.items[i].ID == id {
			return s.items[i], true
		}
	}
	return PendingKeyRequest{}, false
}

// List returns a snapshot of all requests. The slice is a copy; the
// caller may freely mutate it without affecting the store.
func (s *PendingKeyStore) List() []PendingKeyRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slicesCopy(s.items)
}

// CountByStatus is a small helper for the operator metric
// mcp_key_requests_total (Phase 11 § 5 task list); returns the count of
// requests in each known status bucket.
func (s *PendingKeyStore) CountByStatus() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := map[string]int{
		PendingStatusPending:  0,
		PendingStatusApproved: 0,
		PendingStatusRejected: 0,
	}
	for i := range s.items {
		counts[s.items[i].Status]++
	}
	return counts
}

// Decide transitions a pending request to approved or rejected. status
// must be one of PendingStatusApproved or PendingStatusRejected.
// keyID is populated only for approves and identifies the issued
// KeyEntry; for rejects it is ignored. deciderID is the admin's auth
// client_id (audit trail).
//
// Returns ErrPendingNotFound if no request with the given ID exists,
// ErrPendingNotPending if the request has already been decided (guard
// against double-approve / re-email).
func (s *PendingKeyStore) Decide(id, status, deciderID, keyID string) (PendingKeyRequest, error) {
	if status != PendingStatusApproved && status != PendingStatusRejected {
		return PendingKeyRequest{}, fmt.Errorf("%w %q (want %q or %q)",
			ErrPendingInvalidStatus, status, PendingStatusApproved, PendingStatusRejected)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.items {
		if s.items[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return PendingKeyRequest{}, ErrPendingNotFound
	}
	if s.items[idx].Status != PendingStatusPending {
		return PendingKeyRequest{}, ErrPendingNotPending
	}

	now := time.Now().UTC()
	prev := s.items[idx]
	s.items[idx].Status = status
	s.items[idx].DecidedAt = &now
	s.items[idx].DeciderID = deciderID
	if status == PendingStatusApproved {
		s.items[idx].KeyID = keyID
	}

	if err := s.save(s.items); err != nil {
		// Roll back the in-memory mutation on persistence failure to
		// match the Add rollback discipline.
		s.items[idx] = prev
		return PendingKeyRequest{}, err
	}
	return s.items[idx], nil
}

// save writes the supplied snapshot to disk under the same atomic + lock
// discipline as SaveKeysFile in keys.go. Takes a snapshot rather than
// re-reading s.items so the caller's mutex hold time stays bounded.
func (s *PendingKeyStore) save(snapshot []PendingKeyRequest) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pending keys: %w", err)
	}
	return withExclusiveLock(s.path, func() error {
		return writePendingAtomic(s.path, data)
	})
}

// writePendingAtomic is a near-duplicate of writeKeysAtomic in keys.go.
// Kept separate rather than extracting a shared writeAtomic helper to
// minimize the blast radius of this PR — the refactor is mechanical and
// worth its own commit if a third caller ever needs it.
func writePendingAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp pending file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp pending file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp pending file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp pending file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp pending file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp pending file: %w", err)
	}
	tmpPath = "" // committed; suppress the deferred cleanup
	return nil
}

// slicesCopy is a small generic helper so the store can hand callers
// defensive copies without dragging in golang.org/x/exp/slices for one
// call site. Tiny enough to keep local.
func slicesCopy[T any](in []T) []T {
	out := make([]T, len(in))
	copy(out, in)
	return out
}
