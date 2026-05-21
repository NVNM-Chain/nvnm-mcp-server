// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// DefaultLimiterIdleTTL is how long a per-key bucket may sit unused before
// it is eligible for eviction. Long enough that a client doing one request
// per minute never loses its bucket; short enough that transient identities
// (one-shot JWTs, churned admin-created clients, one-off source IPs) do not
// accumulate forever.
const DefaultLimiterIdleTTL = 30 * time.Minute

// DefaultLimiterMaxClients caps the per-key bucket map. Reached only in
// adversarial scenarios (an attacker churning identities or source IPs); on
// hit, the least-recently-used bucket is evicted to make room. Large enough
// that legitimate workloads never trigger eviction.
const DefaultLimiterMaxClients = 10000

type clientBucket struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// keyedRateLimiter is a memory-bounded cache of per-key token buckets.
// The key is opaque (a client ID, a source IP, ...); callers choose what
// to key on. Bounded by maxKeys (LRU eviction) plus an optional TTL
// janitor (Start/Stop).
type keyedRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*clientBucket
	rps     float64
	burst   int
	idleTTL time.Duration
	maxKeys int

	stopCh   chan struct{}
	stopOnce sync.Once
	stopWG   sync.WaitGroup
}

func newKeyedRateLimiter(rps float64, burst int) *keyedRateLimiter {
	return &keyedRateLimiter{
		buckets: make(map[string]*clientBucket),
		rps:     rps,
		burst:   burst,
		idleTTL: DefaultLimiterIdleTTL,
		maxKeys: DefaultLimiterMaxClients,
		stopCh:  make(chan struct{}),
	}
}

// Start launches the background TTL janitor.
func (l *keyedRateLimiter) Start() {
	l.stopWG.Add(1)
	go l.janitor()
}

// Stop signals the janitor (if running) to exit and blocks until it has.
// Safe to call multiple times and from multiple goroutines.
func (l *keyedRateLimiter) Stop() {
	l.stopOnce.Do(func() { close(l.stopCh) })
	l.stopWG.Wait()
}

func (l *keyedRateLimiter) janitor() {
	defer l.stopWG.Done()
	ticker := time.NewTicker(l.idleTTL / 4)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case now := <-ticker.C:
			l.sweep(now)
		}
	}
}

func (l *keyedRateLimiter) sweep(now time.Time) {
	cutoff := now.Add(-l.idleTTL)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if b.lastUsed.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}

// allow consumes a token for key, creating the bucket lazily (with
// LRU eviction when the cap is hit).
func (l *keyedRateLimiter) allow(key string) bool {
	return l.limiterFor(key).Allow()
}

func (l *keyedRateLimiter) limiterFor(key string) *rate.Limiter {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if b, ok := l.buckets[key]; ok {
		b.lastUsed = now
		return b.limiter
	}
	if len(l.buckets) >= l.maxKeys {
		l.evictLRULocked()
	}
	lim := rate.NewLimiter(rate.Limit(l.rps), l.burst)
	l.buckets[key] = &clientBucket{limiter: lim, lastUsed: now}
	return lim
}

func (l *keyedRateLimiter) evictLRULocked() {
	var oldestKey string
	var oldestAt time.Time
	first := true
	for k, b := range l.buckets {
		if first || b.lastUsed.Before(oldestAt) {
			oldestKey = k
			oldestAt = b.lastUsed
			first = false
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

// Size reports the current number of retained buckets.
func (l *keyedRateLimiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
