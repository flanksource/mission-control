package clientcmd

import (
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/clientapi"
)

type playbookRun clientapi.PlaybookRun

type playbookRunStatus clientapi.PlaybookRunStatus

func playbookRuns(runs []clientapi.PlaybookRun) []playbookRun {
	result := make([]playbookRun, len(runs))
	for i, run := range runs {
		result[i] = playbookRun(run)
	}
	return result
}

func (p playbookRun) PrettyRow(_ any) map[string]api.Text {
	row := map[string]api.Text{
		"id":     clicky.Text(p.ID.String()[:8], "font-mono text-xs"),
		"status": playbookRunStatus(p.Status).Pretty(),
	}

	if p.StartTime != nil && p.EndTime != nil {
		row["duration"] = api.Human(p.EndTime.Sub(*p.StartTime), "text-gray-600")
	} else if p.StartTime != nil {
		row["duration"] = api.Human(time.Since(*p.StartTime), "text-blue-600")
	}

	row["created_at"] = api.Human(time.Since(p.CreatedAt), "text-gray-600")
	if p.Error != nil && *p.Error != "" {
		row["error"] = clicky.Text(*p.Error, "text-red-600 text-sm")
	}
	return row
}

func (p playbookRunStatus) Pretty() api.Text {
	var icon, style string
	switch p {
	case playbookRunStatus(clientapi.PlaybookRunStatusCompleted):
		icon, style = "✓", "text-green-600 font-bold"
	case playbookRunStatus(clientapi.PlaybookRunStatusFailed):
		icon, style = "✗", "text-red-600 font-bold"
	case playbookRunStatus(clientapi.PlaybookRunStatusCancelled):
		icon, style = "⊘", "text-gray-600"
	case playbookRunStatus(clientapi.PlaybookRunStatusTimedOut):
		icon, style = "⏱", "text-orange-600"
	case playbookRunStatus(clientapi.PlaybookRunStatusRunning):
		icon, style = "▶", "text-blue-600"
	case playbookRunStatus(clientapi.PlaybookRunStatusRetrying):
		icon, style = "🔄", "text-yellow-600"
	case playbookRunStatus(clientapi.PlaybookRunStatusPendingApproval):
		icon, style = "⏸", "text-purple-600"
	case playbookRunStatus(clientapi.PlaybookRunStatusScheduled), playbookRunStatus(clientapi.PlaybookRunStatusWaiting):
		icon, style = "⏳", "text-cyan-600"
	case playbookRunStatus(clientapi.PlaybookRunStatusSleeping):
		icon, style = "💤", "text-gray-500"
	default:
		icon, style = "•", "text-gray-500"
	}
	return clicky.Text(icon+" ", style).Append(string(p), "capitalize "+style)
}
