package push

import (
	"fmt"
	"io"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
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
		ID  string
		URL string
	}
	if err := ctx.DB().Model(&models.Topology{}).
		Select("id", "spec->'pushLocation'->>'url' as url").Where(duty.LocalFilter).Where("spec ? 'pushLocation'").
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("error querying topologies with location: %w", dbutils.ErrorDetails(err))
	}

	var agentName string
	if api.UpstreamConf.Valid() {
		agentName = api.UpstreamConf.AgentName
	}

	logger.Infof("GOT ROWS = %d", len(rows))
	httpClient := http.NewClient()
	for _, row := range rows {
		opts := query.TopologyOptions{ID: row.ID}
		tree, err := query.Topology(ctx.Context, opts)
		if err != nil {
			ctx.History.AddErrorf("error querying topology tree: %v", err)
			continue
		}

		// TODO: Figure out auth
		req := httpClient.R(ctx).
			Header(echo.HeaderContentType, echo.MIMEApplicationJSON)

		if agentName != "" {
			req.QueryParam("agentName", agentName)
		}

		logger.Infof("PUSH URL Is %v", row)
		endpoint := fmt.Sprintf("%s/push/topology", row.URL)
		resp, err := req.Post(endpoint, tree)
		if err != nil {
			ctx.History.AddErrorf("error pushing topology tree to location[%s]: %v", endpoint, err)
			logger.Infof("YASH ERROR IS %v", err)
			fmt.Printf("YASH ERROR IS %v", err)
			continue
		}

		if !resp.IsOK() {
			respBody, _ := io.ReadAll(resp.Body)
			ctx.History.AddErrorf("non 2xx response for pushing topology tree to location[%s]: %s, %s", row.URL, resp.Status, string(respBody))
			logger.Infof("YASH2 ERROR IS %v", resp.Body)
			fmt.Printf("YASH2 ERROR IS %v", resp.Body)
			continue
		}

		logger.Infof("YASH3 Resp is %v", resp.Status)
		fmt.Printf("YASH3 Resp is %v", resp.Status)
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
