package resource

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// The attempt lock keeps an entry only while it is held or awaited: many
// accounts over time must not grow the map.
func TestKeyedMutex_LeavesNothingBehind(t *testing.T) {
	var k keyedMutex
	var wg sync.WaitGroup
	var counter atomic.Int32
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := k.lock("acct-" + strconv.Itoa(i%7)) // contended keys
			counter.Add(1)
			unlock()
		}()
	}
	wg.Wait()
	if len(k.m) != 0 {
		t.Errorf("%d entries left in the map after every lock was released", len(k.m))
	}
	if counter.Load() != 200 {
		t.Errorf("counter = %d", counter.Load())
	}
}

func TestKeyedMutex_SerialisesOneKey(t *testing.T) {
	var k keyedMutex
	inside, maxInside := 0, 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := k.lock("same")
			mu.Lock()
			inside++
			maxInside = max(maxInside, inside)
			mu.Unlock()
			mu.Lock()
			inside--
			mu.Unlock()
			unlock()
		}()
	}
	wg.Wait()
	if maxInside != 1 {
		t.Errorf("%d holders at once, want 1", maxInside)
	}
}
