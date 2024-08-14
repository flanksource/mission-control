package playbook

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/postq/pg"
)

func ListenPlaybookPGNotify(ctx context.Context) {
	pgNotifyPlaybookSpecUpdated := make(chan string)
	go pg.Listen(ctx, "playbook_spec_updated", pgNotifyPlaybookSpecUpdated)

	for range pgNotifyPlaybookSpecUpdated {
		clearEventPlaybookCache()
	}
}
