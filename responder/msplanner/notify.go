package msplanner

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
	msgraphModels "github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/mitchellh/mapstructure"
)

func (client *MSPlannerClient) NotifyResponderAddComment(ctx context.Context, responder api.Responder, comment string) (string, error) {
	return client.AddComment(responder.ExternalID, comment)
}

func (client *MSPlannerClient) NotifyResponder(ctx context.Context, responder api.Responder) (string, error) {
	var taskOptions MSPlannerTask
	err := mapstructure.Decode(responder.Properties, &taskOptions)
	if err != nil {
		return "", err
	}

	var task msgraphModels.PlannerTaskable
	if task, err = client.CreateTask(taskOptions); err != nil {
		return "", err
	}

	return *task.GetId(), nil
}
