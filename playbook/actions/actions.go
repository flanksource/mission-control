package actions

import (
	"encoding/json"
	"io"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
)

type WebhookRequest struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Params  map[string]string `json:"params"`
	Content string            `json:"content"`
	JSON    types.JSONMap     `json:"json"`
}

func NewWebhookRequest(c echo.Context) (*WebhookRequest, error) {
	headers := make(map[string]string)
	for k := range c.Request().Header {
		headers[k] = c.Request().Header.Get(k)
	}
	params := make(map[string]string)
	for k := range c.QueryParams() {
		params[k] = c.QueryParam(k)
	}
	content, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return nil, err
	}

	whr := WebhookRequest{
		URL:     c.Request().URL.String(),
		Headers: headers,
		Params:  params,
		Content: string(content),
	}

	if c.Request().Header.Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(content, &whr.JSON); err != nil {
			return nil, err
		}
	}
	return &whr, nil
}

// TemplateEnv defines the config and component passed to a playbook run action.
type TemplateEnv struct {
	Config    *models.ConfigItem        `json:"config,omitempty"`
	Component *models.Component         `json:"component,omitempty"`
	Check     *models.Check             `json:"check,omitempty"`
	Playbook  models.Playbook           `json:"playbook"`
	Run       models.PlaybookRun        `json:"run"`
	Action    *models.PlaybookRunAction `json:"action,omitempty"`
	Params    map[string]any            `json:"params,omitempty"`
	Request   types.JSONMap             `json:"request,omitempty"`
	Env       map[string]any            `json:"env,omitempty"`
	GitOps    query.GitOpsSource        `json:"git,omitempty"`

	// User is the user who triggered the playbook run
	User *models.Person `json:"user,omitempty"`

	// Agent belonging to the resource
	Agent *models.Agent `json:"agent,omitempty"`
}

func (t *TemplateEnv) AsMap() map[string]any {
	m := map[string]any{
		"check":     lo.FromPtr(t.Check).AsMap(),
		"component": lo.ToPtr(lo.FromPtr(t.Component)).AsMap(),
		"config":    lo.FromPtr(t.Config).AsMap(),
		"user":      lo.FromPtr(t.User).AsMap(),
		"agent":     lo.FromPtr(t.Agent).AsMap(),
		"action":    lo.FromPtr(t.Action).AsMap(),
		"env":       t.Env,
		"params":    t.Params,
		"playbook":  t.Playbook.AsMap(),
		"git":       t.GitOps.AsMap(),
		"run":       t.Run.AsMap(),
		"request":   t.Request,
	}

	// Inject tags as top level variables
	if t.Config != nil {
		for k, v := range t.Config.Tags {
			if gomplate.IsCelKeyword(k) {
				continue
			}

			if _, ok := m[k]; ok {
				logger.Warnf("skipping tag %s as it already exists in the playbook template environment", k)
				continue
			}

			m[k] = v
		}
	}

	return m
}

func (t *TemplateEnv) GetContext() map[string]any {
	return t.AsMap()
}

func (t *TemplateEnv) String() string {
	b, err := json.Marshal(t.AsMap())
	if err != nil {
		return ""
	}

	return string(b)
}
