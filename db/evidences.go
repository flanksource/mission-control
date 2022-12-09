package db

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"

	"github.com/flanksource/commons/logger"
)

func GetEvidenceScripts() []api.Evidence {
	var evidences []api.Evidence
	openIncidentsSubQuery := Gorm.Table("hypotheses").Select("id").Where("incident_id IN (?)",
		Gorm.Table("incidents").Select("id").Where("closed IS NULL AND resolved IS NULL"))
	err := Gorm.Table("evidences").
		Joins("Config").Joins("Component").
		Where("evidences.hypothesis_id IN (?)", openIncidentsSubQuery).
		Where("evidences.script IS NOT NULL").
		Where("definition_of_done != true").
		Find(&evidences).Error

	if err != nil {
		logger.Errorf("error fetching the evidences: %v", err)
		return evidences
	}
	return evidences
}

func UpdateEvidenceScriptResult(id uuid.UUID, done bool, result string) error {
	return Gorm.Table("evidences").Where("id = ?", id).
		Updates(map[string]any{"definition_of_done": done, "script_result": result}).
		Error
}
