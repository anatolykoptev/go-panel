package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/anatolykoptev/go-panel/identity"
	"github.com/anatolykoptev/go-panel/identity/ratelimit"
	"github.com/redis/go-redis/v9"
)

// compile-time: RedisLimiter satisfies the framework interface.
var _ identity.RateLimiter = (*ratelimit.RedisLimiter)(nil)

func newLimiter(t *testing.T) (*ratelimit.RedisLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return ratelimit.NewRedisLimiter(rdb), mr
}

func TestRedisLimiter_AllowsUpToLimitThenDenies(t *testing.T) {
	lim, _ := newLimiter(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		ok, err := lim.Allow(ctx, "k", 3, time.Minute)
		if err != nil || !ok {
			t.Fatalf("hit %d: ok=%v err=%v, want allowed", i, ok, err)
		}
	}
	ok, err := lim.Allow(ctx, "k", 3, time.Minute)
	if err != nil {
		t.Fatalf("hit 4: err=%v", err)
	}
	if ok {
		t.Errorf("hit 4: want denied (over limit), got allowed")
	}
}

func TestRedisLimiter_WindowResetsAfterExpiry(t *testing.T) {
	lim, mr := newLimiter(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if ok, _ := lim.Allow(ctx, "k", 2, time.Minute); !ok {
			t.Fatalf("hit %d should be allowed", i)
		}
	}
	if ok, _ := lim.Allow(ctx, "k", 2, time.Minute); ok {
		t.Fatalf("over limit should be denied before expiry")
	}
	mr.FastForward(time.Minute + time.Second)
	if ok, err := lim.Allow(ctx, "k", 2, time.Minute); err != nil || !ok {
		t.Errorf("after window expiry: want allowed, ok=%v err=%v", ok, err)
	}
}

// TestRedisLimiter_FailsClosedOnRedisError asserts the security-relevant branch:
// a Redis outage must DENY (return false + error), never silently allow. The
// framework's allowStart treats a non-nil error as "deny", so a fail-OPEN here
// would defeat the throttle during exactly the incident it must hold.
func TestRedisLimiter_FailsClosedOnRedisError(t *testing.T) {
	lim, mr := newLimiter(t)
	mr.Close() // simulate a Redis outage
	ok, err := lim.Allow(context.Background(), "k", 5, time.Minute)
	if err == nil {
		t.Fatalf("want a Redis error after outage, got nil")
	}
	if ok {
		t.Errorf("on Redis error Allow MUST fail closed (false), got allowed")
	}
}
