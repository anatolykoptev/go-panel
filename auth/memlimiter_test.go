package auth_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// compile-time: MemoryLimiter satisfies RateLimiter.
var _ auth.RateLimiter = (*auth.MemoryLimiter)(nil)

func mustAllow(t *testing.T, l *auth.MemoryLimiter, key string, limit int, win time.Duration, want bool) {
	t.Helper()
	got, err := l.Allow(context.Background(), key, limit, win)
	if err != nil {
		t.Fatalf("Allow(%q): unexpected error %v", key, err)
	}
	if got != want {
		t.Fatalf("Allow(%q) = %v, want %v", key, got, want)
	}
}

func TestMemoryLimiter_AllowsUpToLimitThenDenies(t *testing.T) {
	clk := newFakeClock()
	l := auth.NewMemoryLimiter(auth.WithClock(clk.Now))
	for i := 0; i < 3; i++ {
		mustAllow(t, l, "k", 3, time.Minute, true)
	}
	mustAllow(t, l, "k", 3, time.Minute, false)
	mustAllow(t, l, "k", 3, time.Minute, false)
}

func TestMemoryLimiter_ResetsAfterWindow(t *testing.T) {
	clk := newFakeClock()
	l := auth.NewMemoryLimiter(auth.WithClock(clk.Now))
	mustAllow(t, l, "k", 1, time.Minute, true)
	mustAllow(t, l, "k", 1, time.Minute, false)
	clk.Advance(time.Minute - time.Nanosecond)
	mustAllow(t, l, "k", 1, time.Minute, false) // still inside the window
	clk.Advance(time.Nanosecond)                // exactly at window end
	mustAllow(t, l, "k", 1, time.Minute, true)
	mustAllow(t, l, "k", 1, time.Minute, false)
}

func TestMemoryLimiter_DeniedHitsDoNotExtendWindow(t *testing.T) {
	clk := newFakeClock()
	l := auth.NewMemoryLimiter(auth.WithClock(clk.Now))
	mustAllow(t, l, "k", 1, time.Minute, true)
	clk.Advance(30 * time.Second)
	mustAllow(t, l, "k", 1, time.Minute, false)
	clk.Advance(30 * time.Second) // 60s after the FIRST hit
	mustAllow(t, l, "k", 1, time.Minute, true)
}

func TestMemoryLimiter_PerKeyIsolation(t *testing.T) {
	l := auth.NewMemoryLimiter(auth.WithClock(newFakeClock().Now))
	mustAllow(t, l, "a", 1, time.Minute, true)
	mustAllow(t, l, "a", 1, time.Minute, false)
	mustAllow(t, l, "b", 1, time.Minute, true)
}

func TestMemoryLimiter_DifferentRulesPerKeyShareOneInstance(t *testing.T) {
	clk := newFakeClock()
	l := auth.NewMemoryLimiter(auth.WithClock(clk.Now))
	login := auth.RateRule{Limit: 5, Window: time.Minute}
	totp := auth.RateRule{Limit: 3, Window: 10 * time.Minute}
	for i := 0; i < 5; i++ {
		mustAllow(t, l, "login:1.2.3.4", login.Limit, login.Window, true)
	}
	mustAllow(t, l, "login:1.2.3.4", login.Limit, login.Window, false)
	for i := 0; i < 3; i++ {
		mustAllow(t, l, "totp:1.2.3.4", totp.Limit, totp.Window, true)
	}
	mustAllow(t, l, "totp:1.2.3.4", totp.Limit, totp.Window, false)
	clk.Advance(time.Minute) // login window over, totp window not
	mustAllow(t, l, "login:1.2.3.4", login.Limit, login.Window, true)
	mustAllow(t, l, "totp:1.2.3.4", totp.Limit, totp.Window, false)
}

func TestMemoryLimiter_NeverExceedsKeyCap(t *testing.T) {
	const maxKeys = 100
	l := auth.NewMemoryLimiter(auth.WithClock(newFakeClock().Now), auth.WithMaxKeys(maxKeys))
	for i := 0; i < 3*maxKeys; i++ {
		// Long windows: nothing expires, so the cap is only held by eviction.
		mustAllow(t, l, fmt.Sprintf("k%d", i), 1, time.Hour, true)
		if n := l.Len(); n > maxKeys {
			t.Fatalf("after %d inserts tracked keys = %d, want <= %d", i+1, n, maxKeys)
		}
	}
	if n := l.Len(); n != maxKeys {
		t.Fatalf("tracked keys = %d, want exactly cap %d after a flood", n, maxKeys)
	}
}

func TestMemoryLimiter_DefaultCapIs10000(t *testing.T) {
	l := auth.NewMemoryLimiter(auth.WithClock(newFakeClock().Now))
	for i := 0; i < 10_000+50; i++ {
		mustAllow(t, l, fmt.Sprintf("k%d", i), 1, time.Hour, true)
	}
	if n := l.Len(); n != 10_000 {
		t.Fatalf("tracked keys = %d, want default cap 10000", n)
	}
}

func TestMemoryLimiter_NonPositiveMaxKeysKeepsDefault(t *testing.T) {
	l := auth.NewMemoryLimiter(auth.WithClock(newFakeClock().Now), auth.WithMaxKeys(0))
	for i := 0; i < 50; i++ {
		mustAllow(t, l, fmt.Sprintf("k%d", i), 1, time.Hour, true)
	}
	if n := l.Len(); n != 50 {
		t.Fatalf("tracked keys = %d, want 50 (WithMaxKeys(0) must not collapse the cap)", n)
	}
}

func TestMemoryLimiter_EvictsSoonestExpiringAtCap(t *testing.T) {
	l := auth.NewMemoryLimiter(auth.WithClock(newFakeClock().Now), auth.WithMaxKeys(3))
	mustAllow(t, l, "long", 1, 10*time.Minute, true)
	mustAllow(t, l, "short", 1, time.Minute, true) // window ends soonest
	mustAllow(t, l, "mid", 1, 5*time.Minute, true)
	mustAllow(t, l, "new", 1, time.Hour, true) // at cap: must evict "short"
	if n := l.Len(); n != 3 {
		t.Fatalf("tracked keys = %d, want 3", n)
	}
	mustAllow(t, l, "long", 1, 10*time.Minute, false) // survivors keep their state
	mustAllow(t, l, "mid", 1, 5*time.Minute, false)
	mustAllow(t, l, "new", 1, time.Hour, false)
	mustAllow(t, l, "short", 1, time.Minute, true) // evicted: starts a fresh window
}

func TestMemoryLimiter_SweepsExpiredBeforeEvictingLive(t *testing.T) {
	clk := newFakeClock()
	l := auth.NewMemoryLimiter(auth.WithClock(clk.Now), auth.WithMaxKeys(4))
	mustAllow(t, l, "exp1", 1, time.Minute, true)
	mustAllow(t, l, "exp2", 1, time.Minute, true)
	mustAllow(t, l, "live1", 1, time.Hour, true)
	mustAllow(t, l, "live2", 1, time.Hour, true)
	clk.Advance(2 * time.Minute)
	mustAllow(t, l, "new", 1, time.Hour, true)
	if n := l.Len(); n != 3 { // live1, live2, new — both expired entries swept
		t.Fatalf("tracked keys = %d, want 3 (all expired entries swept, no live key evicted)", n)
	}
	mustAllow(t, l, "live1", 1, time.Hour, false)
	mustAllow(t, l, "live2", 1, time.Hour, false)
}

func TestMemoryLimiter_NewKeyAtCapIsAllowedNotDenied(t *testing.T) {
	l := auth.NewMemoryLimiter(auth.WithClock(newFakeClock().Now), auth.WithMaxKeys(2))
	mustAllow(t, l, "a", 1, time.Hour, true)
	mustAllow(t, l, "b", 1, time.Hour, true)
	mustAllow(t, l, "operator", 1, time.Hour, true) // flood of keys must not lock a new one out
}

func TestMemoryLimiter_CancelledContextReturnsError(t *testing.T) {
	l := auth.NewMemoryLimiter(auth.WithClock(newFakeClock().Now))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok, err := l.Allow(ctx, "k", 5, time.Minute)
	if err == nil || ok {
		t.Fatalf("Allow on cancelled ctx = (%v, %v), want (false, ctx error)", ok, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := l.Len(); n != 0 {
		t.Fatalf("cancelled call tracked a key (len=%d)", n)
	}
}

func TestMemoryLimiter_ConcurrentUse(t *testing.T) {
	const (
		maxKeys = 50
		workers = 32
		perW    = 200
		limit   = 40
	)
	l := auth.NewMemoryLimiter(auth.WithMaxKeys(maxKeys))
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perW; i++ {
				if ok, err := l.Allow(context.Background(), "shared", limit, time.Hour); err != nil {
					t.Errorf("Allow: %v", err)
				} else if ok {
					allowed.Add(1)
				}
				if _, err := l.Allow(context.Background(), fmt.Sprintf("w%d-%d", w, i), 1, time.Hour); err != nil {
					t.Errorf("Allow: %v", err)
				}
			}
		}(w)
	}
	wg.Wait()
	if n := l.Len(); n > maxKeys {
		t.Fatalf("tracked keys = %d, want <= %d", n, maxKeys)
	}
	// "shared" may be evicted and restart its window under the flood, so only
	// the lower bound is exact: at least one full window's worth got through.
	if got := allowed.Load(); got < limit {
		t.Fatalf("allowed = %d, want >= %d", got, limit)
	}
}
