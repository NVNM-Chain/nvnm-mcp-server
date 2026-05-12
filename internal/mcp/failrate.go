package mcp

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Defaults for the pre-auth IP failure limiter. Tunable but the defaults
// allow 5 burst attempts and 1 per second sustained per IP -- generous
// enough that retry-on-transient-network mistakes don't lock anyone out,
// tight enough that brute-forcing 32-byte bearer tokens is infeasible.
const (
	DefaultFailRatePerSec     = 1.0
	DefaultFailBurst          = 5
	DefaultFailLimiterIdleTTL = 15 * time.Minute
	DefaultFailMaxIPs         = 10000
)

// IPFailRateLimiter enforces per-source-IP rate limits on failed
// authentications. It sits OUTSIDE AuthMiddleware so that bad-credential
// probes do not bypass the limiter (which the per-client limiter
// inside Auth does, because it requires an established identity).
//
// Two modes:
//
//   - Wrap(next): the limiter pre-checks: if the IP has exhausted its
//     budget, return 429 immediately. Otherwise pass through and let
//     the inner Auth middleware decide; the inner middleware calls
//     Penalize on auth failure to consume a token.
//
// The bucket map is bounded (DefaultFailMaxIPs) and idle entries are
// evicted by a background janitor (Start/Stop), mirroring
// ClientRateLimiter.
type IPFailRateLimiter struct {
	mu         sync.Mutex
	ips        map[string]*clientBucket
	rps        float64
	burst      int
	idleTTL    time.Duration
	maxIPs     int
	stopCh     chan struct{}
	stopWG     sync.WaitGroup
	trustProxy bool
}

// NewIPFailRateLimiter creates a limiter with the given per-IP rate and
// burst budget. trustProxy=true honors X-Forwarded-For (deploy behind
// a reverse proxy that rewrites it); false uses r.RemoteAddr only.
func NewIPFailRateLimiter(rps float64, burst int, trustProxy bool) *IPFailRateLimiter {
	return &IPFailRateLimiter{
		ips:        make(map[string]*clientBucket),
		rps:        rps,
		burst:      burst,
		idleTTL:    DefaultFailLimiterIdleTTL,
		maxIPs:     DefaultFailMaxIPs,
		stopCh:     make(chan struct{}),
		trustProxy: trustProxy,
	}
}

// Start launches the background TTL janitor.
func (l *IPFailRateLimiter) Start() {
	l.stopWG.Add(1)
	go l.janitor()
}

// Stop signals the janitor to exit and blocks until it has.
func (l *IPFailRateLimiter) Stop() {
	select {
	case <-l.stopCh:
	default:
		close(l.stopCh)
	}
	l.stopWG.Wait()
}

func (l *IPFailRateLimiter) janitor() {
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

func (l *IPFailRateLimiter) sweep(now time.Time) {
	cutoff := now.Add(-l.idleTTL)
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, b := range l.ips {
		if b.lastUsed.Before(cutoff) {
			delete(l.ips, ip)
		}
	}
}

// IPFromRequest extracts the source IP per the limiter's trust-proxy
// setting. Exported so the inner auth middleware can use the same
// derivation when calling Penalize.
func (l *IPFailRateLimiter) IPFromRequest(r *http.Request) string {
	if l.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Take the leftmost address; trust-but-verify the proxy
			// removes any client-supplied entries.
			for i, c := range xff {
				if c == ',' {
					return trimSpace(xff[:i])
				}
			}
			return trimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// Wrap returns an http.Handler that pre-checks the per-IP failure
// budget before delegating to next. When the budget is exhausted the
// caller gets a 429 with a JSON error body; otherwise the request
// proceeds and the inner middleware (typically Auth) is expected to
// call Penalize on any failed auth attempt.
func (l *IPFailRateLimiter) Wrap(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := l.IPFromRequest(r)
		if !l.peek(ip) {
			logger.Warn("pre-auth failure-rate limit exceeded",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("ip", ip),
			)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			if _, werr := w.Write([]byte(`{"error":"too many failed auth attempts; retry later"}`)); werr != nil {
				logger.Warn("failrate: write response", slog.String("error", werr.Error()))
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Penalize consumes one token from the IP's failure budget. Called by
// AuthMiddleware on every 401/403. If the budget is already exhausted
// the call is a no-op (the next request will be blocked at Wrap).
func (l *IPFailRateLimiter) Penalize(ip string) {
	if ip == "" {
		return
	}
	lim := l.getLimiter(ip)
	_ = lim.Allow() // consume one token; we don't care if it succeeds
}

// peek reports whether the IP currently has budget available without
// consuming a token. Used by Wrap as a non-destructive pre-check; the
// actual deduction happens in Penalize on auth failure.
func (l *IPFailRateLimiter) peek(ip string) bool {
	if ip == "" {
		return true
	}
	lim := l.getLimiter(ip)
	// rate.Limiter has no peek; use Tokens() which returns remaining
	// fractional tokens. >=1.0 means at least one whole token is
	// available -- the next Penalize will succeed in consuming it.
	return lim.Tokens() >= 1.0
}

func (l *IPFailRateLimiter) getLimiter(ip string) *rate.Limiter {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if b, ok := l.ips[ip]; ok {
		b.lastUsed = now
		return b.limiter
	}

	if len(l.ips) >= l.maxIPs {
		l.evictLRULocked()
	}

	lim := rate.NewLimiter(rate.Limit(l.rps), l.burst)
	l.ips[ip] = &clientBucket{limiter: lim, lastUsed: now}
	return lim
}

func (l *IPFailRateLimiter) evictLRULocked() {
	var oldestID string
	var oldestAt time.Time
	first := true
	for id, b := range l.ips {
		if first || b.lastUsed.Before(oldestAt) {
			oldestID = id
			oldestAt = b.lastUsed
			first = false
		}
	}
	if oldestID != "" {
		delete(l.ips, oldestID)
	}
}

// Size reports the current number of per-IP buckets retained.
func (l *IPFailRateLimiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ips)
}
