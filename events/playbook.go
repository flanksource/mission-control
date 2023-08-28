package events

import "github.com/flanksource/incident-commander/api"

func SavePlaybookRun(ctx *api.Context, event api.Event) error {
	// TODO:
	// See if any playbook is listening on this event.
	// Match the filters
	// If everything goes ok, save the playbook run.
	return nil
}
