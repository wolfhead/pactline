package api

import (
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	rateLimitCapacity      = 30.0
	rateLimitRefillPerSec  = 2.0
	rateLimitPerMinute     = 120
	rateLimitBucketIdleTTL = 15 * time.Minute
)

type LimitDecision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAfter time.Duration
}

type TokenLimiter interface {
	Allow(tokenID uuid.UUID, now time.Time) LimitDecision
}

type tokenBucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

type tokenBucketLimiter struct {
	mu      sync.Mutex
	buckets map[uuid.UUID]tokenBucket
}

func newTokenBucketLimiter() *tokenBucketLimiter {
	return &tokenBucketLimiter{buckets: make(map[uuid.UUID]tokenBucket)}
}

func (l *tokenBucketLimiter) Allow(tokenID uuid.UUID, now time.Time) LimitDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)

	bucket, exists := l.buckets[tokenID]
	if !exists {
		bucket = tokenBucket{tokens: rateLimitCapacity, updated: now}
	}
	if elapsed := now.Sub(bucket.updated); elapsed > 0 {
		bucket.tokens = math.Min(
			rateLimitCapacity,
			bucket.tokens+elapsed.Seconds()*rateLimitRefillPerSec,
		)
		bucket.updated = now
	}
	bucket.lastSeen = now

	allowed := bucket.tokens >= 1
	if allowed {
		bucket.tokens--
	}
	l.buckets[tokenID] = bucket

	resetTokens := rateLimitCapacity - bucket.tokens
	if !allowed {
		resetTokens = 1 - bucket.tokens
	}
	resetAfter := time.Duration(resetTokens / rateLimitRefillPerSec * float64(time.Second))
	if resetAfter < 0 {
		resetAfter = 0
	}
	return LimitDecision{
		Allowed: allowed, Limit: rateLimitPerMinute,
		Remaining: int(math.Floor(bucket.tokens)), ResetAfter: resetAfter,
	}
}

func (l *tokenBucketLimiter) prune(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
}

func (l *tokenBucketLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-rateLimitBucketIdleTTL)
	for tokenID, bucket := range l.buckets {
		if bucket.lastSeen.Before(cutoff) {
			delete(l.buckets, tokenID)
		}
	}
}
