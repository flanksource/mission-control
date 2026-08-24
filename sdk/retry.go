package sdk

import (
	"time"

	internalretry "github.com/flanksource/incident-commander/sdk/internal/retry"
)

func shouldRetry(method, path string, status int, err error) bool {
	return internalretry.ShouldRetry(method, path, status, err)
}

func neverSent(err error) bool {
	return internalretry.NeverSent(err)
}

type RetryPolicy = internalretry.Policy

func WithRetry(retries int, delay time.Duration) ClientOption {
	return func(c *Client) {
		c.retry = RetryPolicy{Retries: retries, Delay: delay}
	}
}
