package clientcmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/clientapi"
)

type jobRun clientapi.JobRun

type jobRunStatus string

func jobRuns(runs []clientapi.JobRun) []jobRun {
	result := make([]jobRun, len(runs))
	for i, run := range runs {
		result[i] = jobRun(run)
	}
	return result
}

func (j jobRun) PrettyRow(_ any) map[string]api.Text {
	id := j.ID
	if len(id) > 8 {
		id = id[:8]
	}
	row := map[string]api.Text{
		"id":     clicky.Text(id, "font-mono text-xs"),
		"name":   clicky.Text(j.Name, "font-medium"),
		"status": jobRunStatus(j.Status).Pretty(),
	}
	if j.ResourceType != "" {
		resource := j.ResourceType
		if j.ResourceID != "" {
			resourceID := j.ResourceID
			if len(resourceID) > 8 {
				resourceID = resourceID[:8]
			}
			resource += " " + resourceID
		}
		row["resource"] = clicky.Text(resource, "text-gray-600 text-sm")
	}
	if j.DurationMillis > 0 {
		row["duration"] = api.Human(time.Duration(j.DurationMillis)*time.Millisecond, "text-gray-600")
	} else if !j.TimeStart.IsZero() && j.TimeEnd == nil {
		row["duration"] = api.Human(time.Since(j.TimeStart), "text-blue-600")
	}
	if !j.TimeStart.IsZero() {
		row["started"] = api.Human(time.Since(j.TimeStart), "text-gray-600")
	}
	if j.ErrorCount > 0 {
		if len(j.Errors) > 0 {
			row["errors"] = clicky.Text(strings.Join(j.Errors, "; "), "text-red-600 text-sm")
		} else {
			row["errors"] = clicky.Text(fmt.Sprintf("%d", j.ErrorCount), "text-red-600 text-sm")
		}
	}
	return row
}

func (j jobRun) Pretty() api.Text {
	items := []api.KeyValuePair{
		{Key: "ID", Value: clicky.Text(j.ID, "font-mono text-xs")},
		{Key: "Name", Value: clicky.Text(j.Name, "font-medium")},
		{Key: "Status", Value: jobRunStatus(j.Status).Pretty()},
	}
	if j.ResourceType != "" {
		items = append(items, api.KeyValuePair{Key: "Resource Type", Value: clicky.Text(j.ResourceType, "text-gray-700")})
	}
	if j.ResourceID != "" {
		items = append(items, api.KeyValuePair{Key: "Resource ID", Value: clicky.Text(j.ResourceID, "font-mono text-xs")})
	}
	if j.AgentID != "" {
		items = append(items, api.KeyValuePair{Key: "Agent ID", Value: clicky.Text(j.AgentID, "font-mono text-xs")})
	}
	if j.Hostname != "" {
		items = append(items, api.KeyValuePair{Key: "Hostname", Value: clicky.Text(j.Hostname, "text-gray-700")})
	}
	items = append(items,
		api.KeyValuePair{Key: "Success", Value: fmt.Sprintf("%d", j.SuccessCount)},
		api.KeyValuePair{Key: "Errors", Value: fmt.Sprintf("%d", j.ErrorCount)},
	)
	if j.DurationMillis > 0 {
		items = append(items, api.KeyValuePair{Key: "Duration", Value: api.Human(time.Duration(j.DurationMillis)*time.Millisecond, "text-gray-600")})
	}
	if !j.TimeStart.IsZero() {
		items = append(items, api.KeyValuePair{Key: "Started", Value: api.Human(time.Since(j.TimeStart), "text-gray-600")})
	}
	if j.TimeEnd != nil {
		items = append(items, api.KeyValuePair{Key: "Ended", Value: api.Human(time.Since(*j.TimeEnd), "text-gray-600")})
	}
	if len(j.Errors) > 0 {
		items = append(items, api.KeyValuePair{Key: "Error Detail", Value: clicky.Text(strings.Join(j.Errors, "\n"), "text-red-600 text-sm")})
	}
	out := clicky.Text("").Append(api.DescriptionList{Items: items})
	if len(j.Details) > 0 {
		detailItems := make([]api.KeyValuePair, 0, len(j.Details))
		for key, value := range j.Details {
			if key == "errors" {
				continue
			}
			detailItems = append(detailItems, api.KeyValuePair{Key: key, Value: fmt.Sprintf("%v", value)})
		}
		if len(detailItems) > 0 {
			out = out.NewLine().Append(clicky.Text("Details", "font-bold text-gray-700")).NewLine().
				Append(api.DescriptionList{Items: detailItems})
		}
	}
	return out
}

func (s jobRunStatus) Pretty() api.Text {
	var icon, style string
	switch s {
	case clientapi.JobStatusSuccess:
		icon, style = "✓", "text-green-600 font-bold"
	case clientapi.JobStatusFailed:
		icon, style = "✗", "text-red-600 font-bold"
	case clientapi.JobStatusWarning:
		icon, style = "⚠", "text-yellow-600 font-bold"
	case clientapi.JobStatusRunning:
		icon, style = "▶", "text-blue-600"
	case clientapi.JobStatusStale:
		icon, style = "⏱", "text-orange-600"
	case clientapi.JobStatusSkipped:
		icon, style = "⊘", "text-gray-600"
	default:
		icon, style = "•", "text-gray-500"
	}
	return clicky.Text(icon+" ", style).Append(string(s), style)
}
