package transport

import (
	"math/rand"
	"time"
)

// Backoff returns an exponential back-off delay with Full Jitter.
//
//	attempt == 0  -> base
//	attempt == 1  -> 2*base   (±50 % jitter)
//	attempt == 2  -> 4*base   (±50 % jitter)
//	...
//
// The value is capped at max.
func backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := base << attempt // base * 2^attempt
	if d > max {
		d = max
	}
	// “Full jitter” – AWS architecture best-practice.
	j := rand.Int63n(int64(d)) // 0 ≤ j < d
	return time.Duration(j)
}

func init() { rand.Seed(time.Now().UnixNano()) }
