package auth

import (
	"testing"
	"time"
)

func TestLoginLimiterBlocksRepeatedFailuresAndResetsAfterWindow(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newLoginLimiter(3, time.Minute, func() time.Time { return now })
	key := "source:user"
	for i := 0; i < 3; i++ {
		if !limiter.Allow(key) {
			t.Fatalf("Allow() blocked before failure %d", i+1)
		}
		limiter.Failure(key)
	}
	if limiter.Allow(key) {
		t.Fatal("Allow() accepted another login after the failure limit")
	}
	now = now.Add(time.Minute + time.Second)
	if !limiter.Allow(key) {
		t.Fatal("Allow() stayed blocked after the window expired")
	}
	limiter.Success(key)
	if !limiter.Allow(key) {
		t.Fatal("Allow() stayed blocked after a successful authentication")
	}
}
