package api

import (
	"sync"
	"time"
)

// loginLimiter is an in-memory sliding-window throttle for credential
// endpoints: max failures per key within window blocks further attempts
// until the oldest failure expires. Per-instance state only (single-binary
// deployment model); a successful login clears the key.
type loginLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	fails  map[string][]time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		max:    max,
		window: window,
		now:    time.Now,
		fails:  map[string][]time.Time{},
	}
}

// allow reports whether the key may attempt a login right now.
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-l.window)
	fails := l.fails[key][:0]
	live := 0
	for _, t := range l.fails[key] {
		if t.After(cutoff) {
			fails = append(fails, t)
			live++
		}
	}
	if len(fails) == 0 {
		delete(l.fails, key)
	} else {
		l.fails[key] = fails
	}
	return live < l.max
}

// fail records a failed attempt for the key.
func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key] = append(l.fails[key], l.now())
}

// reset clears the key's failures (successful login).
func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
