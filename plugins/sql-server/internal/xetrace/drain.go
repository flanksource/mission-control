package xetrace

import (
	"context"
	"time"
)

// poller is the subset of *Session that Drain needs. Keeping the surface
// small lets tests substitute an in-memory fake without spinning up a DB.
type poller interface {
	Poll(ctx context.Context) ([]Event, error)
}

// Drain polls p at the given interval, deduplicates events via Event.Key,
// and delivers each new event to onEvent. It keeps running until ctx is
// cancelled, then performs a final drain so late-arriving events captured
// right before cancellation are not lost.
//
// The drain poll uses a bounded background context (not ctx) so shutdown
// still completes when the caller's context is already Done. onEvent is
// invoked synchronously while dedup state is held — keep it fast.
func Drain(ctx context.Context, p poller, interval time.Duration, onEvent func(Event)) error {
	if interval <= 0 {
		interval = time.Second
	}
	seen := make(map[string]struct{})

	deliver := func(events []Event) {
		for _, e := range events {
			key := e.Key()
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			if onEvent != nil {
				onEvent(e)
			}
		}
	}

	drain := func(ctxPoll context.Context) error {
		events, err := p.Poll(ctxPoll)
		if err != nil {
			return err
		}
		deliver(events)
		return nil
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			finalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := drain(finalCtx)
			cancel()
			return err
		case <-ticker.C:
			pollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := drain(pollCtx)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}
