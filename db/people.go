package db

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/utils"
)

func UpdateUserProperties(ctx *api.Context, userID string, newProps api.PersonProperties) error {
	var current api.Person
	if err := ctx.DB().Table("people").Where("id = ?", userID).First(&current).Error; err != nil {
		return err
	}

	props, err := utils.MergeStructs(current.Properties, newProps)
	if err != nil {
		return err
	}

	return ctx.DB().Table("people").Where("id = ?", userID).Update("properties", props).Error
}

func UpdateIdentityState(ctx *api.Context, id, state string) error {
	return ctx.DB().Table("identities").Where("id = ?", id).Update("state", state).Error
}

func CreateUser(ctx *api.Context, user api.Person) (api.Person, error) {
	err := ctx.DB().Table("people").Create(&user).Error
	return user, err
}
