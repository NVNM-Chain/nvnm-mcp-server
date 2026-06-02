// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func newPendingStoreOnTempFile(t *testing.T) *PendingKeyStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys_pending.json")
	s, err := NewPendingKeyStore(path)
	if err != nil {
		t.Fatalf("NewPendingKeyStore: %v", err)
	}
	return s
}

// TestPendingKeyStore_LoadMissingFileIsEmpty pins that a fresh deployment
// (no keys_pending.json yet) starts with an empty store rather than
// failing — the first POST /api/v1/keys/request must succeed without
// operator pre-provisioning.
func TestPendingKeyStore_LoadMissingFileIsEmpty(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	if got := s.List(); len(got) != 0 {
		t.Errorf("List on missing-file store = %d items, want 0", len(got))
	}
}

func TestPendingKeyStore_AddRoundTrip(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)

	req, err := s.Add("a@example.test", "Acme", "building an agent", "10.0.0.1")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if req.ID == "" {
		t.Error("Add returned empty ID")
	}
	if req.Status != PendingStatusPending {
		t.Errorf("new request Status = %q, want %q", req.Status, PendingStatusPending)
	}
	if req.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set on Add")
	}
	if req.Email != "a@example.test" {
		t.Errorf("Email = %q, want a@example.test", req.Email)
	}
	if req.Company != "Acme" {
		t.Errorf("Company = %q, want Acme", req.Company)
	}
	if req.RemoteAddr != "10.0.0.1" {
		t.Errorf("RemoteAddr = %q, want 10.0.0.1", req.RemoteAddr)
	}

	got, ok := s.Get(req.ID)
	if !ok {
		t.Fatalf("Get(%q) returned !ok after Add", req.ID)
	}
	if got.ID != req.ID || got.Email != req.Email {
		t.Errorf("Get returned %+v, want round-trip of %+v", got, req)
	}
}

// TestPendingKeyStore_PersistsAcrossReopen confirms the on-disk JSON
// survives store restart — load-bearing for the operator workflow
// where the server restarts between an Add and the reviewer decision.
func TestPendingKeyStore_PersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "keys_pending.json")
	s1, err := NewPendingKeyStore(path)
	if err != nil {
		t.Fatalf("NewPendingKeyStore: %v", err)
	}
	added, err := s1.Add("a@example.test", "Acme", "building an agent", "10.0.0.1")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	s2, err := NewPendingKeyStore(path)
	if err != nil {
		t.Fatalf("re-open NewPendingKeyStore: %v", err)
	}
	got, ok := s2.Get(added.ID)
	if !ok {
		t.Fatalf("Get(%q) on reopened store returned !ok", added.ID)
	}
	if got.Email != added.Email || got.Status != PendingStatusPending {
		t.Errorf("reopened request = %+v, want match of %+v", got, added)
	}
}

func TestPendingKeyStore_DecideApprove(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	req, err := s.Add("a@example.test", "", "use case", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	decided, err := s.Decide(req.ID, PendingStatusApproved, "admin-client-id", "issued-key-id")
	if err != nil {
		t.Fatalf("Decide approve: %v", err)
	}
	if decided.Status != PendingStatusApproved {
		t.Errorf("Status = %q, want %q", decided.Status, PendingStatusApproved)
	}
	if decided.DecidedAt == nil || decided.DecidedAt.IsZero() {
		t.Error("DecidedAt should be set after Decide")
	}
	if decided.DeciderID != "admin-client-id" {
		t.Errorf("DeciderID = %q, want %q", decided.DeciderID, "admin-client-id")
	}
	if decided.KeyID != "issued-key-id" {
		t.Errorf("KeyID = %q, want %q", decided.KeyID, "issued-key-id")
	}
}

func TestPendingKeyStore_DecideReject(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	req, _ := s.Add("a@example.test", "", "use case", "")

	decided, err := s.Decide(req.ID, PendingStatusRejected, "admin-client-id", "")
	if err != nil {
		t.Fatalf("Decide reject: %v", err)
	}
	if decided.Status != PendingStatusRejected {
		t.Errorf("Status = %q, want %q", decided.Status, PendingStatusRejected)
	}
	if decided.KeyID != "" {
		t.Errorf("KeyID should be empty on reject, got %q", decided.KeyID)
	}
}

// TestPendingKeyStore_DecideTwiceFails guards the double-approve race:
// two admins clicking approve at nearly the same time must not both
// trigger key issuance + email. The second Decide must hit
// ErrPendingNotPending.
func TestPendingKeyStore_DecideTwiceFails(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	req, _ := s.Add("a@example.test", "", "use case", "")
	if _, err := s.Decide(req.ID, PendingStatusApproved, "admin1", "key-1"); err != nil {
		t.Fatalf("first Decide: %v", err)
	}
	_, err := s.Decide(req.ID, PendingStatusApproved, "admin2", "key-2")
	if !errors.Is(err, ErrPendingNotPending) {
		t.Errorf("second Decide err = %v, want ErrPendingNotPending", err)
	}
}

func TestPendingKeyStore_DecideMissingFails(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	_, err := s.Decide("nope", PendingStatusApproved, "admin", "key")
	if !errors.Is(err, ErrPendingNotFound) {
		t.Errorf("Decide on missing = %v, want ErrPendingNotFound", err)
	}
}

func TestPendingKeyStore_DecideInvalidStatusFails(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	req, _ := s.Add("a@example.test", "", "use case", "")
	_, err := s.Decide(req.ID, "expired", "admin", "")
	if err == nil {
		t.Error("Decide with invalid status should error")
	}
}

func TestPendingKeyStore_CountByStatus(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	r1, _ := s.Add("a@example.test", "", "uc1", "")
	r2, _ := s.Add("b@example.test", "", "uc2", "")
	_, _ = s.Add("c@example.test", "", "uc3", "")
	_, _ = s.Decide(r1.ID, PendingStatusApproved, "admin", "k1")
	_, _ = s.Decide(r2.ID, PendingStatusRejected, "admin", "")

	got := s.CountByStatus()
	want := map[string]int{
		PendingStatusPending:  1,
		PendingStatusApproved: 1,
		PendingStatusRejected: 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("count[%q] = %d, want %d", k, got[k], v)
		}
	}
}

// TestPendingKeyStore_ConcurrentAddIsSafe ramps up parallel Add calls
// to confirm the mutex + atomic-write discipline holds. A bug here
// would manifest as a partial write, a duplicate ID, or a lost record.
func TestPendingKeyStore_ConcurrentAddIsSafe(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	const goroutines = 16
	const perGoroutine = 8

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_, err := s.Add("a@example.test", "", "use case", "")
				if err != nil {
					t.Errorf("Add: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	got := s.List()
	if len(got) != goroutines*perGoroutine {
		t.Errorf("got %d records, want %d", len(got), goroutines*perGoroutine)
	}
	seen := make(map[string]bool, len(got))
	for _, r := range got {
		if seen[r.ID] {
			t.Errorf("duplicate ID: %q", r.ID)
		}
		seen[r.ID] = true
	}
}

// TestPendingKeyStore_ListReturnsCopy pins that mutating the slice
// returned by List does not affect store state — defensive copy
// discipline.
func TestPendingKeyStore_ListReturnsCopy(t *testing.T) {
	t.Parallel()
	s := newPendingStoreOnTempFile(t)
	_, _ = s.Add("a@example.test", "", "use case", "")
	list := s.List()
	list[0].Email = "mutated@example.test"
	again := s.List()
	if again[0].Email == "mutated@example.test" {
		t.Error("List returned shared reference; store state was mutated")
	}
}
