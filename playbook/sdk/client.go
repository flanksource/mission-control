package sdk

import (
	"context"

	"github.com/flanksource/commons/http"
	"github.com/samber/oops"
)

type PlaybookAPI struct {
	*http.Client
}

type RunResponse struct {
	RunID    string `json:"run_id"`
	StartsAt string `json:"starts_at"`
}

type RunParams struct {
	ID          any               `json:"id"`
	ConfigID    any               `json:"config_id"`
	CheckID     any               `json:"check_id"`
	ComponentID any               `json:"component_id"`
	Params      map[string]string `json:"params"`
}

func NewPlaybookClient(base string) PlaybookAPI {
	return PlaybookAPI{
		Client: http.NewClient().BaseURL(base).
			Header("Content-Type", "application/json").
			UserAgent("mission-control/playbook"),
	}
}

func (api *PlaybookAPI) Run(params RunParams) (*RunResponse, error) {
	var response RunResponse

	r, err := api.R(context.Background()).Post("/playbook/run", params)
	if err != nil {
		return nil, err
	}
	// oops := oops.Request(r.RawRequest, true).Response(r.Response, true)
	oops := oops.Response(r.Response, true)

	if !r.IsOK() {
		json, err := r.AsJSON()
		if err != nil {
			return nil, oops.Wrap(err)
		}
		return nil, oops.Errorf(json["error"].(string))
	}

	return &response, oops.Wrap(r.Into(&response))
}
