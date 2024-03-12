package actions

import "github.com/flanksource/duty/models"

// TemplateEnv defines the config and component passed to a playbook run action.
type TemplateEnv struct {
	Config    *models.ConfigItem `json:"config,omitempty"`
	Component *models.Component  `json:"component,omitempty"`
	Check     *models.Check      `json:"check,omitempty"`
	Params    map[string]string  `json:"params,omitempty"`
	Env       map[string]string  `json:"env,omitempty"`
}

func (t *TemplateEnv) AsMap() map[string]any {
	m := map[string]any{
		"config":    nil,
		"component": nil,
		"env":       t.Env,
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
		configEnv, _ := t.Config.TemplateEnv()
		m["config"] = configEnv
	}

	return m
}
