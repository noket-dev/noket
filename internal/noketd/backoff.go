package noketd

import (
	"context"
	"math/rand"
	"time"
)

const (
	minBackoff = time.Second
	maxBackoff = 60 * time.Second
)

// backoff implements 1s -> 60s exponential backoff with jitter.
type backoff struct {
	next time.Duration
}

func (b *backoff) reset() { b.next = 0 }

// wait sleeps for the next delay and reports whether the context is still
// alive afterwards.
func (b *backoff) wait(ctx context.Context) bool {
	delay := b.next
	if delay <= 0 {
		delay = minBackoff
	} else {
		delay *= 2
		if delay > maxBackoff {
			delay = maxBackoff
		}
	}
	b.next = delay

	jitter := time.Duration(rand.Int63n(int64(delay/2) + 1))
	timer := time.NewTimer(delay/2 + jitter)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
