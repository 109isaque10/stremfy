package utils

import (
	"sync"
	"time"
)

type RateLimiter struct {
	tokens       chan struct{} // Semaphore-like channel
	maxTokens    int           // Maximum tokens (capacity of the bucket)
	mu           sync.Mutex    // Mutex for safety
	stopRefillCh chan struct{} // Signal channel to stop refilling tokens
}

// NewRateLimiter initializes a rate limiter with a fixed refill rate
func NewRateLimiter(maxTokens int, refillInterval time.Duration) *RateLimiter {
	rl := &RateLimiter{
		tokens:       make(chan struct{}, maxTokens),
		maxTokens:    maxTokens,
		stopRefillCh: make(chan struct{}),
	}

	// Fill the bucket with initial tokens
	for i := 0; i < maxTokens; i++ {
		rl.tokens <- struct{}{}
	}

	// Start a background goroutine to refill tokens
	go func() {
		ticker := time.NewTicker(refillInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rl.mu.Lock()
				if len(rl.tokens) < rl.maxTokens {
					rl.tokens <- struct{}{} // Add a token if bucket isn't full
				}
				rl.mu.Unlock()
			case <-rl.stopRefillCh:
				return // Stop the refill goroutine
			}
		}
	}()

	return rl
}

// Acquire blocks until a token is available or timeout occurs
func (rl *RateLimiter) Acquire() {
	<-rl.tokens // Blocks until a token is available
}

// Stop stops the refill goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopRefillCh)
}
