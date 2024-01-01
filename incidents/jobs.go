package incidents

import (
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/incidents/responder"
)

var IncidentJobs = []*job.Job{
	EvaluateEvidence, IncidentRules, responder.SyncComments, responder.SyncConfig,
}
