package actions

import "github.com/flanksource/duty/models"

// TemplateEnv defines the config and component passed to a playbook run action.
type TemplateEnv struct {
	Config    *models.ConfigItem `json:"config,omitempty"`
	Component *models.Component  `json:"component,omitempty"`
	Check     *models.Component  `json:"check,omitempty"`
	Params    map[string]string  `json:"params,omitempty"`
}

func (t *TemplateEnv) AsMap() map[string]any {
	return map[string]any{
		"config":    t.Config,
		"component": t.Component,
		"params":    t.Params,
		"check":     t.Check,
	}
}
