package push

import (
	"fmt"
	"io"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty"
	dbutils "github.com/flanksource/duty/db"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	"github.com/labstack/echo/v4"
)

func pushTopologiesWithLocation(ctx job.JobRuntime) error {
	var rows []struct {
		ID           string
		PushLocation string
	}
	if err := ctx.DB().Model(&models.Topology{}).
		Select("id", "spec->'pushLocation'").Where(duty.LocalFilter).Where("spec->>'pushLocation' != ''").
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("error querying topologies with location: %w", dbutils.ErrorDetails(err))
	}

	var agentName string
	if api.UpstreamConf.Valid() {
		agentName = api.UpstreamConf.AgentName
	}

	httpClient := http.NewClient()
	for _, row := range rows {
		opts := query.TopologyOptions{ID: row.ID}
		tree, err := query.Topology(ctx.Context, opts)
		if err != nil {
			ctx.History.AddErrorf("error querying topology tree: %v", err)
			continue
		}

		// TODO: Figure out auth
		resp, err := httpClient.R(ctx).
			Header(echo.HeaderContentType, echo.MIMEApplicationJSON).
			QueryParam("agentName", agentName).
			Post(fmt.Sprintf("%s/push/topology", row.PushLocation), tree)

		if err != nil {
			ctx.History.AddErrorf("error pushing topology tree to location[%s]: %v", row.PushLocation, err)
			continue
		}

		if !resp.IsOK() {
			respBody, _ := io.ReadAll(resp.Body)
			ctx.History.AddErrorf("non 2xx response for pushing topology tree to location[%s]: %s, %s", row.PushLocation, resp.Status, string(respBody))
			continue
		}

		ctx.History.IncrSuccess()
	}

	return nil
}

// PushTopologiesWithLocation periodically pulls playbook actions to run
var PushTopologiesWithLocation = &job.Job{
	Name:       "PushTopologiesWithLocation",
	Schedule:   "@every 5m",
	Retention:  job.RetentionFew,
	JobHistory: true,
	RunNow:     true,
	Singleton:  true,
	Fn:         pushTopologiesWithLocation,
}
