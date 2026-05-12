package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// DefaultLimiterIdleTTL is how long a per-client bucket may sit unused
// before it is eligible for eviction. Long enough that a client doing
// one request per minute never loses their bucket; short enough that
// transient identities (one-shot JWTs, churned admin-created clients)
// do not accumulate forever.
const DefaultLimiterIdleTTL = 30 * time.Minute

// DefaultLimiterMaxClients caps the per-client bucket map. Reached only
// in adversarial scenarios (an attacker churning identities); on hit,
// the least-recently-used bucket is evicted to make room. The value is
// large enough that legitimate workloads never trigger eviction.
const DefaultLimiterMaxClients = 10000

// clientBucket pairs a rate.Limiter with its last-used timestamp so the
// janitor can age out inactive buckets.
type clientBucket struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// ClientRateLimiter enforces per-client token-bucket rate limits on HTTP
// requests. Each authenticated client ID gets its own limiter;
// unauthenticated requests share a single "anonymous" bucket.
//
// The middleware must be placed inside AuthMiddleware so that the
// client ID is already present on the request context.
//
// Memory bound: at most maxClients buckets are retained. When the cap
// is hit, the least-recently-used bucket is evicted. A background
// janitor (Start) also evicts buckets idle longer than idleTTL.
type ClientRateLimiter struct {
	mu         sync.Mutex
	clients    map[string]*clientBucket
	rps        float64
	burst      int
	idleTTL    time.Duration
	maxClients int

	stopCh chan struct{}
	stopWG sync.WaitGroup
}

// NewClientRateLimiter creates a rate limiter with the given
// requests-per-second and burst capacity per client. The bucket cache
// is bounded; call Start to enable the background TTL janitor.
func NewClientRateLimiter(rps float64, burst int) *ClientRateLimiter {
	return &ClientRateLimiter{
		clients:    make(map[string]*clientBucket),
		rps:        rps,
		burst:      burst,
		idleTTL:    DefaultLimiterIdleTTL,
		maxClients: DefaultLimiterMaxClients,
		stopCh:     make(chan struct{}),
	}
}

// Start launches the background janitor that evicts buckets older than
// idleTTL. Safe to call once; subsequent calls are no-ops. Stop the
// janitor with Stop. Callers that do not need TTL-based eviction may
// skip Start -- the cap-based eviction still bounds memory.
func (l *ClientRateLimiter) Start() {
	l.stopWG.Add(1)
	go l.janitor()
}

// Stop signals the background janitor (if running) to exit and blocks
// until it has. Safe to call without Start.
func (l *ClientRateLimiter) Stop() {
	select {
	case <-l.stopCh:
		// already stopped
	default:
		close(l.stopCh)
	}
	l.stopWG.Wait()
}

func (l *ClientRateLimiter) janitor() {
	defer l.stopWG.Done()
	// Sweep at quarter of TTL so an entry never lives more than 1.25x TTL.
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

// sweep evicts buckets whose lastUsed is older than idleTTL.
func (l *ClientRateLimiter) sweep(now time.Time) {
	cutoff := now.Add(-l.idleTTL)
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, b := range l.clients {
		if b.lastUsed.Before(cutoff) {
			delete(l.clients, id)
		}
	}
}

// Middleware returns an http.Handler that enforces the per-client rate
// limit. Requests that exceed the limit receive HTTP 429 with a JSON
// error body.
func (l *ClientRateLimiter) Middleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := auth.ClientIDFromContext(r.Context())
		if clientID == "" {
			clientID = "__anonymous__"
		}

		limiter := l.getLimiter(clientID)
		if !limiter.Allow() {
			logger.Warn("MCP rate limit exceeded",
				slog.String("client_id", clientID),
				slog.String("remote_addr", r.RemoteAddr),
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			if encErr := json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded",
			}); encErr != nil {
				logger.Warn("rate limit: encode error response", slog.String("error", encErr.Error()))
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getLimiter returns the rate limiter for the given client ID, creating
// one lazily if it does not yet exist. When the cap is hit, the
// least-recently-used bucket is evicted before inserting.
func (l *ClientRateLimiter) getLimiter(clientID string) *rate.Limiter {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if b, ok := l.clients[clientID]; ok {
		b.lastUsed = now
		return b.limiter
	}

	if len(l.clients) >= l.maxClients {
		l.evictLRULocked()
	}

	lim := rate.NewLimiter(rate.Limit(l.rps), l.burst)
	l.clients[clientID] = &clientBucket{limiter: lim, lastUsed: now}
	return lim
}

// evictLRULocked removes the single least-recently-used bucket. Caller
// must hold l.mu. O(n) over the map but only invoked when the cap is
// hit, which only happens under adversarial churn.
func (l *ClientRateLimiter) evictLRULocked() {
	var oldestID string
	var oldestAt time.Time
	first := true
	for id, b := range l.clients {
		if first || b.lastUsed.Before(oldestAt) {
			oldestID = id
			oldestAt = b.lastUsed
			first = false
		}
	}
	if oldestID != "" {
		delete(l.clients, oldestID)
	}
}

// Size reports the current number of per-client buckets retained.
// Intended for tests and operational metrics.
func (l *ClientRateLimiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}
