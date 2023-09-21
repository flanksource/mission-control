package playbook

import (
	"github.com/flanksource/duty/duty/pg"
	"github.com/flanksource/incident-commander/api"
)

func ListenPlaybookPGNotify(ctx api.Context) {
	pgNotifyPlaybookSpecUpdated := make(chan string)
	go pg.Listen(ctx, "playbook_spec_updated", pgNotifyPlaybookSpecUpdated)

	for range pgNotifyPlaybookSpecUpdated {
		clearEventPlaybookCache()
	}
}
