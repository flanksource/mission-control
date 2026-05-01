package api

import "github.com/flanksource/duty/query"

type ScraperInfo struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Namespace   string              `json:"namespace,omitempty"`
	Description string              `json:"description,omitempty"`
	Source      string              `json:"source,omitempty"`
	Types       []string            `json:"types"`
	SpecHash    string              `json:"specHash"`
	CreatedBy   string              `json:"createdBy,omitempty"`
	CreatedAt   string              `json:"createdAt"`
	UpdatedAt   string              `json:"updatedAt,omitempty"`
	GitOps      *query.GitOpsSource `json:"gitops,omitempty"`
}
