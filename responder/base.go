package responder

import (
	"context"

	"github.com/flanksource/incident-commander/db"
)

func ProcessQueue() error {

	ctx := context.Background()

	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}

	_, err = conn.Exec(context.Background(), "listen responder_updates")
	if err != nil {
		return err
	}

	for {
		notif, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}

		// Fetch the data via responder_id which is in the payload
		_ = notif.Payload

		// TODO: Process the palyoad below in a goroutine

	}
}
