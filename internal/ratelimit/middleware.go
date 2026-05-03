package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

// RateLimitMiddleware applies rate limiting to requests
func RateLimitMiddleware(limiter *TokenBucketLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP
			clientIP := extractClientIP(r)

			// Check rate limit
			if !limiter.Check(clientIP) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "rate_limited",
					"retry_after": 60,
				})
				return
			}

			// Request allowed
			next.ServeHTTP(w, r)
		})
	}
}

// StartCleanupRoutine starts a goroutine that periodically cleans up idle buckets
func StartCleanupRoutine(limiter *TokenBucketLimiter, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			limiter.Cleanup()
		}
	}()
}

// extractClientIP gets the client IP from X-Forwarded-For or RemoteAddr
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	forwardedFor := r.Header.Get("X-Forwarded-For")
	if forwardedFor != "" {
		// Take the first IP if multiple are present
		ips := strings.Split(forwardedFor, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
