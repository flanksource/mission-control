package actions

import (
	"encoding/json"
	"io"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/labstack/echo/v4"
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
	Config    *models.ConfigItem `json:"config,omitempty"`
	Component *models.Component  `json:"component,omitempty"`
	Check     *models.Check      `json:"check,omitempty"`
	Playbook  models.Playbook    `json:"playbook"`
	Run       models.PlaybookRun `json:"run"`
	Params    map[string]any     `json:"params,omitempty"`
	Request   types.JSONMap      `json:"request"`
	Env       map[string]string  `json:"env,omitempty"`

	// User is the user who triggered the playbook run
	User *models.Person `json:"user,omitempty"`

	// Agent belonging to the resource
	Agent *models.Agent `json:"agent,omitempty"`
}

func (t *TemplateEnv) AsMap() map[string]any {
	m := map[string]any{
		"check":     nil,
		"component": nil,
		"config":    nil,
		"user":      nil,
		"env":       t.Env,
		"params":    t.Params,
		"playbook":  t.Playbook.AsMap(),
		"run":       t.Run.AsMap(),
		"request":   t.Request,
	}

	if t.Agent != nil {
		m["agent"] = t.Agent.AsMap()
	}
	if t.User != nil {
		m["user"] = t.User.AsMap()
	}
	if t.Check != nil {
		m["check"] = t.Check.AsMap()
	}
	if t.Component != nil {
		m["component"] = t.Component.AsMap()
	}
	if t.Config != nil {
		configEnv, _ := t.Config.TemplateEnv()
		m["config"] = configEnv
	}

	return m
}
