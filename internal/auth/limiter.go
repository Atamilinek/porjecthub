package auth

import (
	"crypto/sha256"
	"sync"
	"time"
)

const maxLoginBuckets = 10000

type loginAttempt struct {
	started     time.Time
	lastUpdated time.Time
	failures    int
	blockedTill time.Time
}

type LoginLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	clock    func() time.Time
	attempts map[[32]byte]loginAttempt
}

func newLoginLimiter(limit int, window time.Duration, clock func() time.Time) *LoginLimiter {
	if limit < 1 {
		limit = 5
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	if clock == nil {
		clock = time.Now
	}
	return &LoginLimiter{limit: limit, window: window, clock: clock, attempts: make(map[[32]byte]loginAttempt)}
}

// NewLoginLimiter creates an in-memory per-key failed-login limiter.
func NewLoginLimiter(limit int, window time.Duration) *LoginLimiter {
	return newLoginLimiter(limit, window, time.Now)
}

func (l *LoginLimiter) Allow(key string) bool {
	now := l.clock()
	bucket := loginBucket(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	entry, exists := l.attempts[bucket]
	if !exists {
		return true
	}
	if !entry.blockedTill.IsZero() && now.Before(entry.blockedTill) {
		return false
	}
	if now.Sub(entry.started) >= l.window {
		delete(l.attempts, bucket)
		return true
	}
	return entry.failures < l.limit
}

func (l *LoginLimiter) Failure(key string) {
	now := l.clock()
	bucket := loginBucket(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	entry, exists := l.attempts[bucket]
	if !exists || now.Sub(entry.started) >= l.window {
		entry = loginAttempt{started: now}
	}
	entry.failures++
	entry.lastUpdated = now
	if entry.failures >= l.limit {
		entry.blockedTill = entry.started.Add(l.window)
	}
	l.attempts[bucket] = entry
}

func (l *LoginLimiter) Success(key string) {
	bucket := loginBucket(key)
	l.mu.Lock()
	delete(l.attempts, bucket)
	l.mu.Unlock()
}

func (l *LoginLimiter) prune(now time.Time) {
	for key, entry := range l.attempts {
		if now.Sub(entry.lastUpdated) >= l.window {
			delete(l.attempts, key)
		}
	}
	if len(l.attempts) <= maxLoginBuckets {
		return
	}
	for key, entry := range l.attempts {
		if now.Sub(entry.lastUpdated) > l.window/2 {
			delete(l.attempts, key)
		}
		if len(l.attempts) <= maxLoginBuckets {
			break
		}
	}
}

func loginBucket(key string) [32]byte { return sha256.Sum256([]byte(key)) }
