// Package middleware provides HTTP middleware for the carrier-gateway.
package middleware

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type contextKey int

const clientIDKey contextKey = iota

// unauthorizedBody is pre-marshalled to avoid per-rejection allocations.
var unauthorizedBody = []byte(`{"error":"UNAUTHORIZED: missing or invalid API key"}` + "\n")

// tooManyRequestsBody is pre-marshalled for auth failure rate limiting.
var tooManyRequestsBody = []byte(`{"error":"TOO_MANY_REQUESTS: too many failed authentication attempts"}` + "\n")

// authFailureLimiter tracks per-IP rate limits on authentication failures.
// Burst of 10 failures allowed; tokens refill at ~1 per 6 seconds.
type authFailureLimiter struct {
	ips      sync.Map
	lastSeen sync.Map // IP -> time.Time
	rate     rate.Limit
	burst    int
}

func newAuthFailureLimiter() *authFailureLimiter {
	return &authFailureLimiter{
		rate:  rate.Every(6 * time.Second), // 1 token per 6 seconds
		burst: 10,
	}
}

// tryRecordFailure atomically checks the failure budget and consumes a
// token. Returns false if the IP has exhausted its budget (caller should
// return 429). This eliminates the TOCTOU between a separate check and
// record step.
func (l *authFailureLimiter) tryRecordFailure(ip string) bool {
	lim := l.getOrCreate(ip)
	return lim.Allow()
}

// isExhausted returns true if the IP has no remaining failure budget.
// Used as a pre-check before attempting authentication to fast-reject
// exhausted IPs without doing the constant-time key comparison.
func (l *authFailureLimiter) isExhausted(ip string) bool {
	lim := l.getOrCreate(ip)
	return lim.Tokens() < 1
}

func (l *authFailureLimiter) getOrCreate(ip string) *rate.Limiter {
	l.lastSeen.Store(ip, time.Now())
	if v, ok := l.ips.Load(ip); ok {
		return v.(*rate.Limiter)
	}
	lim := rate.NewLimiter(l.rate, l.burst)
	actual, _ := l.ips.LoadOrStore(ip, lim)
	return actual.(*rate.Limiter)
}

// startCleanup runs a background goroutine that evicts entries older than ttl
// every interval. Returns a stop function that must be called on shutdown.
func (l *authFailureLimiter) startCleanup(interval, ttl time.Duration) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				l.lastSeen.Range(func(key, value any) bool {
					if now.Sub(value.(time.Time)) > ttl {
						l.ips.Delete(key)
						l.lastSeen.Delete(key)
					}
					return true
				})
			}
		}
	}()
	return cancel
}

// ClientIDFromContext returns the truncated API key identifier stored by
// RequireAPIKey, or empty string if the request was unauthenticated.
func ClientIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(clientIDKey).(string); ok {
		return v
	}
	return ""
}

// RequireAPIKey rejects requests that do not carry a valid Bearer token.
// Keys in skipPaths bypass authentication (e.g. /healthz, /metrics).
// Each key is identified by its first 8 characters + "..." for logging.
func RequireAPIKey(next http.Handler, keys []string, skipPaths []string, log *slog.Logger) (http.Handler, func()) {
	// Pre-build parallel slices: keyBytes for comparison, keyIDs for context.
	keyBytes := make([][]byte, len(keys))
	keyIDs := make([]string, len(keys))
	for i, k := range keys {
		keyBytes[i] = []byte(k)
		id := k
		if len(id) > 8 {
			id = id[:8] + "..."
		}
		keyIDs[i] = id
	}

	skip := make(map[string]bool, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = true
	}

	limiter := newAuthFailureLimiter()
	stopCleanup := limiter.startCleanup(60*time.Second, 5*time.Minute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		if limiter.isExhausted(ip) {
			log.Warn("auth rate limited",
				slog.String("path", r.URL.Path),
				slog.String("remote", r.RemoteAddr),
			)
			writeTooManyRequests(w)
			return
		}

		raw := r.Header.Get("Authorization")
		token := strings.TrimPrefix(raw, "Bearer ")
		if token == "" || token == raw {
			limiter.tryRecordFailure(ip)
			log.Warn("auth failed: missing bearer token",
				slog.String("path", r.URL.Path),
				slog.String("remote", r.RemoteAddr),
			)
			writeUnauthorized(w)
			return
		}

		// Check all keys to avoid leaking which index matched via timing.
		tokenB := []byte(token)
		matchIdx := -1
		for i, kb := range keyBytes {
			if subtle.ConstantTimeCompare(tokenB, kb) == 1 {
				matchIdx = i
			}
		}

		if matchIdx < 0 {
			limiter.tryRecordFailure(ip)
			log.Warn("auth failed: invalid API key",
				slog.String("path", r.URL.Path),
				slog.String("remote", r.RemoteAddr),
			)
			writeUnauthorized(w)
			return
		}

		ctx := context.WithValue(r.Context(), clientIDKey, keyIDs[matchIdx])
		next.ServeHTTP(w, r.WithContext(ctx))
	}), stopCleanup
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write(unauthorizedBody)
}

func writeTooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "60")
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write(tooManyRequestsBody)
}
