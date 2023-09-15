package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sethvargo/go-retry"
)

// listenToPostgresNotify listens to postgres notifications
// and will retry on failure for dbReconnectMaxAttempt times
func ListenToPostgresNotify(pool *pgxpool.Pool, channel string, dbReconnectMaxDuration, dbReconnectBackoffBaseDuration time.Duration, pgNotify chan<- string) {
	var listen = func(ctx context.Context, pgNotify chan<- string) error {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("error acquiring database connection: %v", err)
		}
		defer conn.Release()

		_, err = conn.Exec(ctx, fmt.Sprintf("LISTEN %s", channel))
		if err != nil {
			return fmt.Errorf("error listening to database notifications: %v", err)
		}
		logger.Debugf("listening to database notifications: %s", channel)

		for {
			n, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				return fmt.Errorf("error listening to database notifications: %v", err)
			}

			pgNotify <- n.Payload
		}
	}

	// retry on failure.
	for {
		backoff := retry.WithMaxDuration(dbReconnectMaxDuration, retry.NewExponential(dbReconnectBackoffBaseDuration))
		err := retry.Do(context.TODO(), backoff, func(ctx context.Context) error {
			if err := listen(ctx, pgNotify); err != nil {
				return retry.RetryableError(err)
			}

			return nil
		})

		logger.Errorf("failed to connect to database: %v", err)
	}
}
