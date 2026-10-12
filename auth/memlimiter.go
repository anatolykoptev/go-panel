package auth

import (
	"context"
	"sync"
	"time"
)

// defaultMemoryLimiterMaxKeys bounds MemoryLimiter's tracked keys when
// WithMaxKeys is not given.
const defaultMemoryLimiterMaxKeys = 10000

// memWindow is one key's fixed window.
type memWindow struct {
	count int
	end   time.Time // the window is over once now >= end
}

// MemoryLimiter is an in-process fixed-window RateLimiter for deployments that
// have no shared store (a single admin process; Redis optional or absent). It
// satisfies RateLimiter, so BcryptConfig.RateLimiter takes it directly, and one
// instance can serve LoginRate and TOTPRate at once because limit and window
// are supplied per Allow call, not at construction.
//
// Fixed window per key: the first hit starts the window, hits beyond limit are
// denied until it ends. Denied hits do not extend the window.
//
// Memory is hard-capped (WithMaxKeys, default 10000). A NEW key arriving at the
// cap first sweeps expired entries, then, if still full, evicts the entry whose
// window ends soonest. A new key is never denied because the map is full:
// fail-closed on capacity would let a flood of spoofed keys lock the operator
// out. The cost is that an evicted key restarts with a fresh window, so under a
// flood larger than the cap an attacker can reset their own counter; process-
// local limiting is a brute-force speed bump, not a distributed guarantee.
//
// Allow returns ctx.Err() when ctx is done and never errors otherwise. Safe for
// concurrent use.
type MemoryLimiter struct {
	mu      sync.Mutex
	hits    map[string]memWindow
	maxKeys int
	now     func() time.Time
}

// MemoryLimiterOption configures NewMemoryLimiter.
type MemoryLimiterOption func(*MemoryLimiter)

// WithMaxKeys sets the cap on tracked keys. n <= 0 is ignored (the default of
// 10000 stays), so a misconfigured zero cannot collapse the limiter.
func WithMaxKeys(n int) MemoryLimiterOption {
	return func(l *MemoryLimiter) {
		if n > 0 {
			l.maxKeys = n
		}
	}
}

// withClock injects the clock; exposed to tests via export_test.go.
func withClock(now func() time.Time) MemoryLimiterOption {
	return func(l *MemoryLimiter) { l.now = now }
}

// NewMemoryLimiter returns a ready-to-use MemoryLimiter.
func NewMemoryLimiter(opts ...MemoryLimiterOption) *MemoryLimiter {
	l := &MemoryLimiter{
		hits:    make(map[string]memWindow),
		maxKeys: defaultMemoryLimiterMaxKeys,
		now:     time.Now,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

var _ RateLimiter = (*MemoryLimiter)(nil)

// Allow implements RateLimiter.
func (l *MemoryLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	w, tracked := l.hits[key]
	if !tracked {
		if len(l.hits) >= l.maxKeys {
			l.makeRoom(now)
		}
	}
	if !tracked || !now.Before(w.end) {
		w = memWindow{end: now.Add(window)}
	}
	allowed := w.count < limit
	if allowed {
		w.count++ // denied hits are not counted: no overflow, no window extension
	}
	l.hits[key] = w
	return allowed, nil
}

// makeRoom frees at least one slot for a new key in a single pass: it drops
// every expired window and remembers the live window ending soonest; if
// nothing expired, that one is evicted. Caller holds l.mu.
func (l *MemoryLimiter) makeRoom(now time.Time) {
	var (
		soonestKey string
		soonestEnd time.Time
		haveLive   bool
		swept      bool
	)
	for k, w := range l.hits {
		if !now.Before(w.end) {
			delete(l.hits, k)
			swept = true
			continue
		}
		if !haveLive || w.end.Before(soonestEnd) {
			soonestKey, soonestEnd, haveLive = k, w.end, true
		}
	}
	if !swept && haveLive {
		delete(l.hits, soonestKey)
	}
}
