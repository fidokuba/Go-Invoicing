package ratelimit

import (
	"strconv"
	"testing"
	"time"
)

// newTestLimiter returns a Limiter on a manually advanced clock.
func newTestLimiter(every time.Duration, burst, maxKeys int) (*Limiter, *time.Time) {
	l := New(every, burst, maxKeys)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	l.lastSweep = now
	return l, &now
}

func TestLimiter_BurstThenLimitedWithRetryAfter(t *testing.T) {
	l, _ := newTestLimiter(6*time.Second, 3, 100)

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d within the burst was refused", i+1)
		}
	}

	ok, retryAfter := l.Allow("a")
	if ok {
		t.Fatal("expected the request beyond the burst to be refused")
	}
	if retryAfter != 6*time.Second {
		t.Errorf("expected Retry-After of 6s (one token's refill), got %v", retryAfter)
	}

	// A refused request doesn't consume a token or push the wait further out.
	if _, again := l.Allow("a"); again != 6*time.Second {
		t.Errorf("expected repeated refusals to keep reporting 6s, got %v", again)
	}
}

func TestLimiter_RetryAfterRoundsUpToWholeSeconds(t *testing.T) {
	l, now := newTestLimiter(1500*time.Millisecond, 1, 100)
	l.Allow("a")
	*now = now.Add(200 * time.Millisecond)

	if _, retryAfter := l.Allow("a"); retryAfter != 2*time.Second {
		t.Errorf("expected 1.3s to round up to 2s, got %v", retryAfter)
	}
}

func TestLimiter_RefillsOverTime(t *testing.T) {
	l, now := newTestLimiter(6*time.Second, 1, 100)
	l.Allow("a")

	if ok, _ := l.Allow("a"); ok {
		t.Fatal("expected the second immediate request to be refused")
	}
	*now = now.Add(6 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Error("expected a request to be allowed once a token has refilled")
	}
}

func TestLimiter_KeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(time.Minute, 1, 100)

	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a's first request refused")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a's second request allowed")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("expected b to have its own bucket, unaffected by a")
	}
}

func TestLimiter_IdleBucketsAreSwept(t *testing.T) {
	l, now := newTestLimiter(time.Second, 2, 100)
	for i := 0; i < 50; i++ {
		l.Allow("key-" + strconv.Itoa(i))
	}
	if l.Len() != 50 {
		t.Fatalf("expected 50 buckets, got %d", l.Len())
	}

	// Long enough for every bucket to refill completely, and past the
	// sweep interval: the next Allow drops them all but its own.
	*now = now.Add(2 * time.Minute)
	l.Allow("fresh")

	if l.Len() != 1 {
		t.Errorf("expected idle buckets to be swept, leaving 1, got %d", l.Len())
	}
}

func TestLimiter_RecentlyUsedBucketsSurviveSweep(t *testing.T) {
	l, now := newTestLimiter(time.Hour, 1, 100)
	l.Allow("busy") // empties the bucket; it needs an hour to refill

	*now = now.Add(2 * time.Minute)
	l.Allow("other") // triggers a sweep

	if ok, _ := l.Allow("busy"); ok {
		t.Error("expected busy's still-empty bucket to survive the sweep and keep limiting")
	}
}

// A flood of distinct keys can't grow memory past maxKeys: once full,
// unseen keys share one overflow bucket — still limited, never unlimited.
func TestLimiter_MemoryIsBoundedAndOverflowIsStillLimited(t *testing.T) {
	l, _ := newTestLimiter(time.Hour, 1, 10)

	for i := 0; i < 10; i++ {
		l.Allow("known-" + strconv.Itoa(i))
	}

	allowed := 0
	for i := 0; i < 1000; i++ {
		if ok, _ := l.Allow("attacker-" + strconv.Itoa(i)); ok {
			allowed++
		}
	}

	if l.Len() != 10 {
		t.Errorf("expected the bucket map to stay at maxKeys (10), got %d", l.Len())
	}
	if allowed != 1 {
		t.Errorf("expected overflow keys to share one bucket (1 allowed), got %d", allowed)
	}

	// Keys already tracked keep their own buckets.
	if ok, _ := l.Allow("known-0"); ok {
		t.Error("expected known-0's own (empty) bucket to still apply")
	}
}
