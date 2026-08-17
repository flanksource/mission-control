package main

import (
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
)

type catalogInsightSeverity string

type catalogInsightType string

type catalogInsightDetailView struct {
	ID            uuid.UUID                          `json:"id"`
	ConfigID      uuid.UUID                          `json:"config_id"`
	ScraperID     *uuid.UUID                         `json:"scraper_id,omitempty"`
	Analyzer      string                             `json:"analyzer"`
	Message       string                             `json:"message,omitempty"`
	Summary       string                             `json:"summary,omitempty"`
	Status        string                             `json:"status,omitempty"`
	Severity      catalogInsightSeverity             `json:"severity,omitempty"`
	AnalysisType  catalogInsightType                 `json:"analysis_type,omitempty"`
	Analysis      map[string]any                     `json:"analysis,omitempty"`
	Properties    *clientapi.CatalogProperties       `json:"properties,omitempty"`
	Source        string                             `json:"source,omitempty"`
	FirstObserved *time.Time                         `json:"first_observed,omitempty"`
	LastObserved  *time.Time                         `json:"last_observed,omitempty"`
	IsPushed      bool                               `json:"is_pushed,omitempty"`
	Config        *clientapi.CatalogChangeConfig     `json:"config,omitempty"`
	Evidences     []clientapi.CatalogInsightEvidence `json:"evidences,omitempty"`
}

func catalogInsightDetailViews(details []clientapi.CatalogInsightDetail) []catalogInsightDetailView {
	views := make([]catalogInsightDetailView, len(details))
	for i, detail := range details {
		views[i] = catalogInsightDetailViewOf(detail)
	}
	return views
}

func catalogInsightDetailViewOf(detail clientapi.CatalogInsightDetail) catalogInsightDetailView {
	return catalogInsightDetailView{
		ID:            detail.ID,
		ConfigID:      detail.ConfigID,
		ScraperID:     detail.ScraperID,
		Analyzer:      detail.Analyzer,
		Message:       detail.Message,
		Summary:       detail.Summary,
		Status:        detail.Status,
		Severity:      catalogInsightSeverity(detail.Severity),
		AnalysisType:  catalogInsightType(detail.AnalysisType),
		Analysis:      detail.Analysis,
		Properties:    detail.Properties,
		Source:        detail.Source,
		FirstObserved: detail.FirstObserved,
		LastObserved:  detail.LastObserved,
		IsPushed:      detail.IsPushed,
		Config:        detail.Config,
		Evidences:     detail.Evidences,
	}
}

func (s catalogInsightSeverity) Pretty() api.Text {
	switch s {
	case "critical":
		return clicky.Text(string(s), "uppercase font-bold text-red-600 bg-red-50")
	case "high":
		return clicky.Text(string(s), "uppercase font-bold text-orange-600 bg-orange-50")
	case "medium":
		return clicky.Text(string(s), "capitalize text-yellow-700 bg-yellow-50")
	case "low":
		return clicky.Text(string(s), "capitalize text-blue-600 bg-blue-50")
	case "info":
		return clicky.Text(string(s), "capitalize text-gray-600")
	default:
		return clicky.Text(string(s), "text-gray-500")
	}
}

func (a catalogInsightType) Pretty() api.Text {
	var icon, style string
	switch a {
	case "security":
		icon, style = "🔒", "text-red-700 bg-red-50"
	case "cost":
		icon, style = "💰", "text-green-700 bg-green-50"
	case "performance":
		icon, style = "⚡", "text-yellow-700 bg-yellow-50"
	case "availability":
		icon, style = "🟢", "text-blue-700 bg-blue-50"
	case "reliability":
		icon, style = "🔄", "text-purple-700 bg-purple-50"
	case "compliance":
		icon, style = "✅", "text-indigo-700 bg-indigo-50"
	case "technical_debt":
		icon, style = "⚙️", "text-gray-700 bg-gray-50"
	case "recommendation":
		icon, style = "💡", "text-cyan-700 bg-cyan-50"
	case "integration":
		icon, style = "🔗", "text-teal-700 bg-teal-50"
	default:
		icon, style = "📊", "text-gray-600"
	}
	return clicky.Text(icon+" ", style).Append(string(a), "capitalize "+style)
}
