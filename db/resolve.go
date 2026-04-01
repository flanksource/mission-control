package db

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

func ResolveExternalUsers(ctx context.Context, query string, limit int) ([]models.ExternalUser, error) {
	db := ctx.DB().Table("external_users").Where("deleted_at IS NULL")

	if id, err := uuid.Parse(query); err == nil {
		db = db.Where("id = ?", id)
	} else {
		pattern := "%" + query + "%"
		db = db.Where("name ILIKE ? OR email ILIKE ? OR ? = ANY(aliases)", pattern, pattern, query)
	}

	var users []models.ExternalUser
	if err := db.Limit(limit).Order("name").Find(&users).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to resolve external users")
	}
	return users, nil
}

func ResolveExternalGroups(ctx context.Context, query string, limit int) ([]models.ExternalGroup, error) {
	db := ctx.DB().Table("external_groups").Where("deleted_at IS NULL")

	if id, err := uuid.Parse(query); err == nil {
		db = db.Where("id = ?", id)
	} else {
		pattern := "%" + query + "%"
		db = db.Where("name ILIKE ? OR ? = ANY(aliases)", pattern, query)
	}

	var groups []models.ExternalGroup
	if err := db.Limit(limit).Order("name").Find(&groups).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to resolve external groups")
	}
	return groups, nil
}

func ResolveConfigItems(ctx context.Context, query, configType string, limit int) ([]map[string]any, error) {
	db := ctx.DB().Table("config_items").
		Select("id, name, type, health, status").
		Where("deleted_at IS NULL")

	if id, err := uuid.Parse(query); err == nil {
		db = db.Where("id = ?", id)
	} else {
		pattern := "%" + query + "%"
		db = db.Where("name ILIKE ?", pattern)
	}

	if configType != "" {
		db = db.Where("type = ?", configType)
	}

	var items []map[string]any
	if err := db.Limit(limit).Order("name").Find(&items).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to resolve config items")
	}
	return items, nil
}
