package worker

import (
	"github.com/flanksource/incident-commander/responder"
)

func Init() {
	responder.ProcessQueue()

	select {}
}
