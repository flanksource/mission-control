package actions

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type Github struct {
}

type GithubResponse struct {
}

type githubWorkflowDispatchRequest struct {
	Ref   string         `json:"ref"`
	Input map[string]any `json:"inputs"`
}

func (t *githubWorkflowDispatchRequest) SetInput(input string) error {
	if len(input) == 0 {
		return nil
	}

	return json.Unmarshal([]byte(input), &t.Input)
}

func (t *Github) Run(ctx context.Context, spec v1.GithubAction) (*GithubResponse, error) {
	token, err := ctx.GetEnvValueFromCache(spec.Token)
	if err != nil {
		return nil, fmt.Errorf("could not get github token from env: %v", err)
	}

	var output GithubResponse
	for _, workflow := range spec.Workflows {
		postBody := githubWorkflowDispatchRequest{
			Ref: workflow.Ref,
		}
		if err := postBody.SetInput(workflow.Input); err != nil {
			return nil, fmt.Errorf("provided workflow input is not in JSON format: %s", workflow.Input)
		}

		endpoint := fmt.Sprintf("%s/%s/actions/workflows/%s/dispatches", spec.Username, spec.Repo, workflow.ID)
		response, err := http.NewClient().
			BaseURL("https://api.github.com/repos").
			Header("Accept", "application/vnd.github+json").
			Header("Authorization", fmt.Sprintf("Bearer %s", token)).
			Header("X-GitHub-Api-Version", "2022-11-28").
			R(ctx).Post(endpoint, postBody)
		if err != nil {
			return nil, err
		}

		if response.StatusCode != 204 {
			body, _ := response.AsString()
			return nil, fmt.Errorf("(workflow: %s) github api returned status code %d. %s", workflow.ID, response.StatusCode, body)
		}
	}

	return &output, nil
}
