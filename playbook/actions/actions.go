package actions

import "github.com/flanksource/duty/models"

// TemplateEnv defines the config and component passed to a playbook run action.
type TemplateEnv struct {
	Config    *models.ConfigItem `json:"config,omitempty"`
	Component *models.Component  `json:"component,omitempty"`
	Check     *models.Check      `json:"check,omitempty"`
	Params    map[string]string  `json:"params,omitempty"`
}

func (t *TemplateEnv) AsMap() map[string]any {
	m := map[string]any{
		"config":    nil,
		"component": nil,
		"params":    t.Params,
		"check":     nil,
	}

	if t.Check != nil {
		m["check"] = t.Check.AsMap()
	}
	if t.Component != nil {
		m["component"] = t.Component.AsMap()
	}
	if t.Config != nil {
		m["config"] = t.Config.AsMap()
	}

	return m
}
