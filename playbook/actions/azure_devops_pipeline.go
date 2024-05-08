package actions

import (
	"fmt"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type AzureDevopsPipeline struct {
}

func (t *AzureDevopsPipeline) Run(ctx context.Context, spec v1.AzureDevopsPipelineAction) (map[string]any, error) {
	token, err := ctx.GetEnvValueFromCache(spec.Token, ctx.GetNamespace())
	if err != nil {
		return nil, fmt.Errorf("could not get azure devops token from env: %v", err)
	}

	pipeline := spec.Pipeline

	request := http.NewClient().BaseURL("https://dev.azure.com").Auth("", token).
		R(ctx).QueryParam("api-version", "7.1-preview.1").Header("Content-Type", "application/json")
	if pipeline.Version != "" {
		request = request.QueryParam("api-version", pipeline.Version)
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/pipelines/%s/runs", spec.Org, spec.Project, pipeline.ID)
	response, err := request.Post(endpoint, spec.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to run pipeline: %w", err)
	}

	if response.StatusCode != 200 {
		body, _ := response.AsString()
		return nil, fmt.Errorf("api returned status code %d for pipeline: %s (response:%s)", response.StatusCode, pipeline.ID, body)
	}

	var body map[string]any
	if err := response.Into(&body); err != nil {
		return nil, fmt.Errorf("failed to read API response: %w", err)
	}

	return body, nil
}
