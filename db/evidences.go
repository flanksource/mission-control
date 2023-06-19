package db

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"

	"github.com/flanksource/commons/logger"
)

type Hypothesis struct {
	api.Hypothesis
	Incident api.Incident `json:"incident,omitempty" gorm:"foreignKey:IncidentID;references:ID"`
}

type EvidenceScriptInput struct {
	api.Evidence
	ConfigAnalysis api.ConfigAnalysis `json:"config_analysis,omitempty" gorm:"foreignKey:ConfigAnalysisID;references:ID"`
	ConfigItem     api.ConfigItem     `json:"config,omitempty" gorm:"foreignKey:ConfigID;references:ID"`
	Check          api.Check          `json:"check,omitempty" gorm:"foreignKey:CheckID;references:ID"`
	Component      api.Component      `json:"component,omitempty"`
	Hypothesis     Hypothesis
}

func GetEvidenceScripts() []EvidenceScriptInput {
	var evidences []EvidenceScriptInput
	incidentsSubQuery := Gorm.Table("incidents").Select("id").Where("closed IS NULL")
	hypothesesSubQuery := Gorm.Table("hypotheses").Select("id").Where("incident_id IN (?)", incidentsSubQuery)
	err := Gorm.Table("evidences").
		Joins("ConfigItem").
		Joins("ConfigAnalysis").
		Joins("Check").
		Joins("Component").
		Joins("Hypothesis").
		Preload("Hypothesis.Incident").
		Where("evidences.hypothesis_id IN (?)", hypothesesSubQuery).
		Where("evidences.script IS NOT NULL").
		Where("evidences.script != ''").
		Find(&evidences).Error

	if err != nil {
		logger.Errorf("error fetching the evidences: %v", err)
		return nil
	}

	return evidences
}

func UpdateEvidenceScriptResult(id uuid.UUID, done bool, result string) error {
	return Gorm.Table("evidences").Where("id = ?", id).
		Updates(map[string]any{"done": done, "script_result": result}).
		Error
}
