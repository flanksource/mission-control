package db

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/utils"
)

func UpdateUserProperties(userID string, newProps api.PersonProperties) error {
	var current api.Person
	if err := Gorm.Table("people").Where("id = ?", userID).First(&current).Error; err != nil {
		return err
	}

	props, err := utils.MergeStructs(current.Properties, newProps)
	if err != nil {
		return err
	}

	return Gorm.Table("people").Where("id = ?", userID).Update("properties", props).Error
}

func UpdateIdentityState(id, state string) error {
	return Gorm.Table("identities").Where("id = ?", id).Update("state", state).Error
}
