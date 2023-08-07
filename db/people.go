package db

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
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

func GetOrCreateUser(ctx *api.Context, user api.Person) (api.Person, error) {
	if err := ctx.DB().Table("people").Where("email = ?", user.Email).Find(&user).Error; err != nil {
		return api.Person{}, err
	}
	if user.ID != uuid.Nil {
		return user, nil
	}
	err := ctx.DB().Table("people").Create(&user).Error
	return user, err
}
