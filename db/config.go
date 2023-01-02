package db

func LookupRelatedConfigIDs(configID string, maxDepth int) ([]string, error) {
	var configIDs []string

	var ancestoryRows []struct {
		ChildID  string
		ParentID string
	}
	if err := Gorm.Raw(`SELECT child_id, parent_id FROM lookup_config_ancestory(?, ?)`, configID, maxDepth).
		Scan(&ancestoryRows).Error; err != nil {
		return configIDs, err
	}
	for _, row := range ancestoryRows {
		configIDs = append(configIDs, row.ChildID, row.ParentID)
	}

	var relatedRows []string
	if err := Gorm.Raw(`SELECT id FROM lookup_config_relations(?)`, configID).
		Scan(&relatedRows).Error; err != nil {
		return configIDs, err
	}
	configIDs = append(configIDs, relatedRows...)

	return configIDs, nil
}
