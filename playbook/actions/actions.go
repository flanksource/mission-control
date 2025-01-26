package actions

import (
	"encoding/json"
	"io"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/db"
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

func (t *TemplateEnv) AsMap(ctx context.Context) map[string]any {
	output := map[string]any{
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

	// Placeholders for commonly used fields of the resource
	// If a resource exists, they'll be filled up below
	output["name"] = ""
	output["status"] = ""
	output["health"] = ""
	output["labels"] = map[string]string{}
	tags := map[string]string{}

	if resource := t.SelectableResource(); resource != nil {
		// set the alias name/status/health/labels/tags of the resource
		output["name"] = resource.GetName()
		if status, err := resource.GetStatus(); err == nil {
			output["status"] = status
		}
		if health, err := resource.GetHealth(); err == nil {
			output["health"] = health
		}
		if table, ok := resource.(models.TaggableModel); ok {
			tags = table.GetTags()
		}
		if table, ok := resource.(models.LabelableModel); ok {
			output["labels"] = table.GetLabels()
		}
	}

	if ctx.DB() != nil {
		if distinctTags, err := db.GetDistinctTags(ctx); err != nil {
			logger.Errorf("failed to get distinct tags for notification cel variable: %w", err)
		} else {
			for _, tag := range distinctTags {
				if _, ok := tags[tag]; !ok {
					tags[tag] = ""
				}
			}
		}
	}

	output["tags"] = tags

	// Inject tags as top level variables
	for k, v := range tags {
		if !gomplate.IsValidCELIdentifier(k) {
			logger.V(9).Infof("skipping tag %s as it is not a valid CEL identifier", k)
			continue
		}

		if _, ok := output[k]; ok {
			logger.V(9).Infof("skipping tag %s as it already exists in the playbook template environment", k)
			continue
		}

		output[k] = v
	}

	return output
}

func (t *TemplateEnv) JSON(ctx context.Context) string {
	b, err := json.Marshal(t.AsMap(ctx))
	if err != nil {
		return ""
	}

	return string(b)
}

func (t *TemplateEnv) SelectableResource() types.ResourceSelectable {
	if t.Component != nil {
		return t.Component
	}
	if t.Config != nil {
		return t.Config
	}
	if t.Check != nil {
		return t.Check
	}
	return nil
}
