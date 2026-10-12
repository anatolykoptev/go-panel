package auth

// Test-only seams into MemoryLimiter internals.

// WithClock injects a fake clock.
var WithClock = withClock

// Len reports the number of tracked keys.
func (l *MemoryLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}
