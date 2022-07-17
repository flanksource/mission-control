package responder

import (
	"github.com/flanksource/incident-commander/responder/jira"
)

func ProcessQueue() {

	// Use conn.Exec(ctx, "listen responder_updates")
	// Fetch the data via responder_id which is in the payload
	// Select correct responder based on the properties column

	// Initialize all the clients on start up ... use init() ?
	jira.NewClient("", "", "")
}
