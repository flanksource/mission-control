package harness

import (
	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

// Playbook is a playbook, created through the database API as one saved from the UI.
type Playbook struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Source    string         `json:"source"`
	Spec      map[string]any `json:"spec"`

	// Approvers are the emails of the people who must approve each run before it starts
	Approvers []string `json:"-"`
}

// ScopeType implements Resource.
func (*Playbook) ScopeType() string { return "playbook" }

// Prepare implements Resource. A playbook runs on any config, and runs a script that does nothing.
func (p *Playbook) Prepare(f *Fixture) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Name == "" {
		p.Name = f.Name("playbook")
	}
	if p.Namespace == "" {
		p.Namespace = f.Namespace
	}
	if p.Source == "" {
		p.Source = "UI"
	}
	if p.Spec == nil {
		p.Spec = map[string]any{
			"configs": []any{map[string]any{"name": "*"}},
			"actions": []any{map[string]any{"name": "noop", "exec": map[string]any{"script": "true"}}},
		}
	}
	if len(p.Approvers) > 0 {
		p.Spec["approval"] = map[string]any{"approvers": map[string]any{"people": p.Approvers}}
	}
}

// CreateRequest implements Resource.
func (p *Playbook) CreateRequest() Request {
	return POST("/db/playbooks", p)
}

// Path implements Resource.
func (p *Playbook) Path() string { return "/db/playbooks?id=eq." + p.ID }

// PlaybookRun is a run of a playbook on a config, started through /playbook/run.
type PlaybookRun struct {
	ID         string `json:"id"`
	PlaybookID string `json:"playbook_id"`
	ConfigID   string `json:"config_id"`
	Status     string `json:"status"`
}

// ScopeType implements Resource. Runs are selected by their playbook.
func (*PlaybookRun) ScopeType() string { return "playbook" }

// Prepare implements Resource.
func (*PlaybookRun) Prepare(*Fixture) {}

// CreateRequest implements Resource.
func (r *PlaybookRun) CreateRequest() Request {
	return POST("/playbook/run", map[string]any{"id": r.PlaybookID, "config_id": r.ConfigID})
}

// Created implements Identified.
func (r *PlaybookRun) Created(resp Response) {
	var created struct {
		RunID string `json:"run_id"`
	}
	gomega.Expect(resp.Decode(&created)).To(gomega.Succeed())
	r.ID = created.RunID
}

// Path implements Resource.
func (r *PlaybookRun) Path() string { return "/db/playbook_runs?id=eq." + r.ID }

// RunOf is a run of the playbook on the config, to start.
func RunOf(playbook *Playbook, config *Config) *PlaybookRun {
	return &PlaybookRun{PlaybookID: playbook.ID, ConfigID: config.ID}
}
