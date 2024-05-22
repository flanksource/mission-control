package actions

import "github.com/flanksource/duty/models"

// TemplateEnv defines the config and component passed to a playbook run action.
type TemplateEnv struct {
	Config    *models.ConfigItem `json:"config,omitempty"`
	Component *models.Component  `json:"component,omitempty"`
	Check     *models.Check      `json:"check,omitempty"`
	Playbook  models.Playbook    `json:"playbook"`
	Run       models.PlaybookRun `json:"run"`
	Params    map[string]string  `json:"params,omitempty"`
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
