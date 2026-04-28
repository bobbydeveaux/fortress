package embedder

import (
	"context"
	"fmt"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}

type retryableFunc func(ctx context.Context) ([][]float32, bool, error)

func withRetries(ctx context.Context, maxAttempts int, fn retryableFunc) ([][]float32, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := sleepBeforeRetry(ctx, attempt); err != nil {
			return nil, err
		}
		result, shouldRetry, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		if !shouldRetry {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("after retries: %w", lastErr)
}

func sleepBeforeRetry(ctx context.Context, attempt int) error {
	if attempt == 0 {
		return nil
	}
	delay := time.Duration(500*(1<<attempt)) * time.Millisecond
	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
