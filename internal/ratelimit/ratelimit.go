package ratelimit

import (
	"sync"
	"time"
)

// bucket represents a token bucket for a specific client
type bucket struct {
	tokens    float64
	lastRefill time.Time
}

// TokenBucketLimiter implements token bucket rate limiting per IP
type TokenBucketLimiter struct {
	buckets          map[string]*bucket
	requestsPerMinute int
	mu               sync.RWMutex
}

// NewTokenBucketLimiter creates a new rate limiter
func NewTokenBucketLimiter(requestsPerMinute int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		buckets:           make(map[string]*bucket),
		requestsPerMinute: requestsPerMinute,
	}
}

// Check returns true if the request should be allowed, false if rate limited
func (tbl *TokenBucketLimiter) Check(clientIP string) bool {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()

	now := time.Now()
	b, exists := tbl.buckets[clientIP]

	if !exists {
		// Create new bucket for this client
		b = &bucket{
			tokens:     float64(tbl.requestsPerMinute),
			lastRefill: now,
		}
		tbl.buckets[clientIP] = b
	}

	// Refill tokens based on time elapsed
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		refillRate := float64(tbl.requestsPerMinute) / 60.0 // requests per second
		tokensToAdd := refillRate * elapsed
		b.tokens = min(float64(tbl.requestsPerMinute), b.tokens+tokensToAdd)
		b.lastRefill = now
	}

	// Check if we have tokens available
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}

	return false
}

// Cleanup removes idle buckets (idle > 30 minutes)
func (tbl *TokenBucketLimiter) Cleanup() {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()

	now := time.Now()
	idleThreshold := 30 * time.Minute

	for ip, b := range tbl.buckets {
		if now.Sub(b.lastRefill) > idleThreshold {
			delete(tbl.buckets, ip)
		}
	}
}

// min returns the minimum of two floats
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
