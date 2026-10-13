// Package retry retries startup operations while dependencies come up.
package retry

import (
	"context"
	"log"
	"time"
)

// Do calls fn up to attempts times with exponential backoff starting at base.
func Do(ctx context.Context, name string, attempts int, base time.Duration, fn func() error) error {
	var err error
	delay := base
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i == attempts {
			break
		}
		log.Printf("%s: attempt %d/%d failed: %v (retrying in %s)", name, i, attempts, err, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
	return err
}
