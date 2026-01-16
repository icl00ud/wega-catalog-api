package client

import (
	"context"
	"time"
)

// RateLimiter controls request rate
type RateLimiter struct {
	ticker   *time.Ticker
	requests chan struct{}
}

// NewRateLimiter creates a rate limiter with specified rate
func NewRateLimiter(requestsPerSecond float64) *RateLimiter {
	interval := time.Duration(float64(time.Second) / requestsPerSecond)

	rl := &RateLimiter{
		ticker:   time.NewTicker(interval),
		requests: make(chan struct{}),
	}

	go func() {
		for range rl.ticker.C {
			select {
			case rl.requests <- struct{}{}:
			default:
			}
		}
	}()

	return rl
}

// Wait blocks until rate limit allows next request
func (rl *RateLimiter) Wait(ctx context.Context) error {
	select {
	case <-rl.requests:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop stops the rate limiter
func (rl *RateLimiter) Stop() {
	rl.ticker.Stop()
	close(rl.requests)
}
