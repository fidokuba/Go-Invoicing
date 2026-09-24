// Package ratelimit is a small, in-process, keyed token-bucket limiter
// (Milestone 13 Part 4) for the few endpoints where repeated requests are
// a realistic abuse or resource risk. Each key (a client address or a
// user) gets its own golang.org/x/time/rate bucket.
//
// State lives in this process's memory only: it suits the current
// single-instance deployment. With several instances each enforces its
// own limits independently, and a restart forgets all state — see
// Milestone 13 Part 5.
package ratelimit

import (
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter holds one token bucket per key, with bounded memory:
//
//   - A bucket that has been idle long enough to refill completely is
//     indistinguishable from a brand-new one, so it can be dropped without
//     changing any decision. Such buckets are swept at most once per
//     sweepInterval, inline on Allow — no background goroutine.
//   - At most maxKeys buckets are ever held. When full (even after a
//     sweep), requests from unseen keys share a single overflow bucket
//     rather than growing the map — a flood of distinct keys (e.g.
//     spoofed-looking IPv6 addresses) can neither exhaust memory nor
//     escape limiting, while existing keys keep their own buckets.
type Limiter struct {
	limit rate.Limit
	burst int

	maxKeys       int
	idleTTL       time.Duration
	sweepInterval time.Duration

	mu        sync.Mutex
	buckets   map[string]*bucket
	overflow  *rate.Limiter
	lastSweep time.Time
	now       func() time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New returns a Limiter allowing each key a sustained every-interval
// rate with the given burst, holding at most maxKeys buckets.
func New(every time.Duration, burst, maxKeys int) *Limiter {
	limit := rate.Every(every)

	// Time for an empty bucket to refill completely; after that long idle
	// a bucket carries no information. A little slack avoids dropping a
	// bucket a moment before it's actually full.
	idleTTL := time.Duration(float64(burst)*float64(every)) + time.Second

	return &Limiter{
		limit:         limit,
		burst:         burst,
		maxKeys:       maxKeys,
		idleTTL:       idleTTL,
		sweepInterval: time.Minute,
		buckets:       make(map[string]*bucket),
		overflow:      rate.NewLimiter(limit, burst),
		now:           time.Now,
	}
}

// Allow reports whether a request for key may proceed now, consuming a
// token if so. If not, retryAfter is how long until one would be
// available, rounded up to whole seconds (for a Retry-After header).
func (l *Limiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	if now.Sub(l.lastSweep) >= l.sweepInterval {
		l.sweep(now)
	}

	limiter := l.bucketFor(key, now)

	reservation := limiter.ReserveN(now, 1)
	delay := reservation.DelayFrom(now)
	if delay == 0 {
		return true, 0
	}

	// Not allowed: give the token back rather than queueing for it.
	reservation.CancelAt(now)

	return false, time.Duration(math.Ceil(delay.Seconds())) * time.Second
}

// bucketFor returns key's bucket, creating it if there is room, and
// otherwise the shared overflow bucket. Caller holds l.mu.
func (l *Limiter) bucketFor(key string, now time.Time) *rate.Limiter {
	if b, ok := l.buckets[key]; ok {
		b.lastSeen = now
		return b.limiter
	}

	if len(l.buckets) >= l.maxKeys {
		// An early sweep may free room — but at most once a second, so a
		// flood of new keys against a full map can't force a full scan on
		// every request.
		if now.Sub(l.lastSweep) >= time.Second {
			l.sweep(now)
		}
		if len(l.buckets) >= l.maxKeys {
			return l.overflow
		}
	}

	b := &bucket{limiter: rate.NewLimiter(l.limit, l.burst), lastSeen: now}
	l.buckets[key] = b

	return b.limiter
}

// sweep drops every bucket idle long enough to have refilled completely.
// Caller holds l.mu.
func (l *Limiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) >= l.idleTTL {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}

// Len reports how many per-key buckets are currently held.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.buckets)
}
