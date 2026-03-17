package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm/clause"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func GetAllApplications(ctx context.Context) ([]models.Application, error) {
	var apps []models.Application
	if err := ctx.DB().Where("deleted_at IS NULL").Find(&apps).Error; err != nil {
		return nil, err
	}

	return apps, nil
}

func FindApplication(ctx context.Context, namespace, name string) (*models.Application, error) {
	var app models.Application
	if err := ctx.DB().Where("deleted_at IS NULL").Where("name = ? AND namespace = ?", name, namespace).Find(&app).Error; err != nil {
		return nil, err
	}

	if app.ID == uuid.Nil {
		return nil, nil
	}

	return &app, nil
}

func PersistApplicationFromCRD(ctx context.Context, obj *v1.Application) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	spec, err := json.Marshal(obj.Spec)
	if err != nil {
		return err
	}

	app := models.Application{
		ID:          uid,
		Name:        obj.Name,
		Namespace:   obj.Namespace,
		Description: obj.Spec.Description,
		Spec:        string(spec),
		Source:      models.SourceCRD,
	}

	return ctx.DB().Save(&app).Error
}

func DeleteApplication(ctx context.Context, id string) error {
	return ctx.Transaction(func(txCtx context.Context, span trace.Span) error {
		if err := txCtx.DB().Model(&models.Application{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		if err := txCtx.DB().Model(&models.ConfigScraper{}).Where("application_id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		// Delete custom roles
		if err := txCtx.DB().Where("application_id = ?", id).Delete(&models.ExternalRole{}).Error; err != nil {
			return err
		}

		return nil
	})
}

func DeleteStaleApplication(ctx context.Context, newer *v1.Application) error {
	return ctx.Transaction(func(txCtx context.Context, span trace.Span) error {
		if err := ctx.DB().Model(&models.Application{}).
			Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
			Where("deleted_at IS NULL").
			Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		if err := txCtx.DB().Model(&models.ConfigScraper{}).Where("application_id = ?", newer.GetID()).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		// Delete custom roles
		if err := txCtx.DB().Where("application_id = ?", newer.GetID()).Delete(&models.ExternalRole{}).Error; err != nil {
			return err
		}

		return nil
	})
}

// GetDistinctUserRoleFromConfigAccess returns deduplicated users and roles
// for the given config IDs, aggregating time fields across configs.
func GetDistinctUserRoleFromConfigAccess(ctx context.Context, configIDs []uuid.UUID) ([]api.UserAndRole, error) {
	if len(configIDs) == 0 {
		return nil, nil
	}

	var users []api.UserAndRole
	if err := ctx.DB().
		Table("config_access_summary").
		Select(`external_user_id::text as id, "user" as name, email, role, user_type as auth_type,
			MIN(created_at) as created_at,
			MAX(last_signed_in_at) as last_login,
			MAX(last_reviewed_at) as last_access_review`).
		Where("config_id IN (?)", configIDs).
		Group(`external_user_id, "user", email, role, user_type`).
		Find(&users).Error; err != nil {
		return nil, err
	}

	return users, nil
}

type ApplicationBackup struct {
	ID          uuid.UUID `json:"id"`
	ConfigID    uuid.UUID `json:"config_id"`
	Name        string    `json:"name"`
	ConfigType  string    `json:"type"`
	ConfigClass string    `json:"config_class"`
	ChangeType  string    `json:"change_type"`
	CreatedAt   time.Time `json:"created_at"`
	Size        string    `json:"size"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
}

func GetApplicationBackups(ctx context.Context, configIDs []uuid.UUID, changeTypes []string) ([]ApplicationBackup, error) {
	if len(configIDs) == 0 {
		return nil, nil
	}

	var changes []ApplicationBackup
	selectColumns := []string{
		"config_changes.id",
		"config_changes.config_id",
		"config_items.name",
		"config_items.type",
		"config_items.config_class",
		"config_changes.change_type",
		"config_changes.created_at",
		"config_changes.source",
		"config_changes.details->>'status' AS status",
		"config_changes.details->>'size' AS size",
	}
	if err := ctx.DB().
		Model(&models.ConfigChange{}).
		Select(selectColumns).
		Joins("LEFT JOIN config_items ON config_items.id = config_changes.config_id").
		Where("config_changes.config_id IN (?) AND config_changes.change_type IN (?)", configIDs, changeTypes).
		Order("config_changes.created_at").
		Find(&changes).Error; err != nil {
		return nil, err
	}

	filteredChanges := dedupBackupChanges(changes)
	slices.Reverse(filteredChanges)

	return filteredChanges, nil
}

// removes BackupStarted events if there's a corresponding BackupCompleted event
func dedupBackupChanges(changes []ApplicationBackup) []ApplicationBackup {
	if len(changes) == 0 {
		return changes
	}

	// Track the last change for each (ConfigID, Source) pair
	lastChange := make(map[string]*ApplicationBackup)

	var result []ApplicationBackup
	for i := range changes {
		change := &changes[i]
		key := fmt.Sprintf("%s|%s", change.ConfigID, change.Source)

		if lastChange[key] != nil {
			// If we have a previous change for this (ConfigID, Source) pair
			prevChange := lastChange[key]

			if prevChange.ChangeType == "BackupStarted" && change.ChangeType == "BackupCompleted" {
				// Remove the previous BackupStarted from result and add the BackupCompleted
				// Find and remove the previous BackupStarted
				for j := len(result) - 1; j >= 0; j-- {
					if result[j].ConfigID == prevChange.ConfigID &&
						result[j].Source == prevChange.Source &&
						result[j].ChangeType == "BackupStarted" &&
						result[j].CreatedAt.Equal(prevChange.CreatedAt) {
						result = append(result[:j], result[j+1:]...)
						break
					}
				}
				result = append(result, *change)
			} else {
				// Different pattern, just add the current change
				result = append(result, *change)
			}
		} else {
			// First change for this (ConfigID, Source) pair
			result = append(result, *change)
		}

		lastChange[key] = change
	}

	return result
}

type ApplicationRestore struct {
	ID          uuid.UUID `json:"id"`
	ConfigID    uuid.UUID `json:"config_id"`
	Name        string    `json:"name"`
	ConfigType  string    `json:"type"`
	ConfigClass string    `json:"config_class"`
	ChangeType  string    `json:"change_type"`
	CreatedAt   time.Time `json:"created_at"`
	Size        string    `json:"size"`
	Status      string    `json:"status"`
}

func GetApplicationRestores(ctx context.Context, configIDs []uuid.UUID, changeTypes []string) ([]ApplicationRestore, error) {
	if len(configIDs) == 0 {
		return nil, nil
	}

	var changes []ApplicationRestore
	selectColumns := []string{
		"config_changes.id",
		"config_changes.config_id",
		"config_items.name",
		"config_items.type",
		"config_items.config_class",
		"config_changes.change_type",
		"config_changes.created_at",
		"config_changes.details->>'status' AS status",
		"config_changes.details->>'size' AS size",
	}
	if err := ctx.DB().
		Model(&models.ConfigChange{}).
		Select(selectColumns).
		Joins("LEFT JOIN config_items ON config_items.id = config_changes.config_id").
		Where("config_changes.config_id IN (?) AND config_changes.change_type IN (?)", configIDs, changeTypes).
		Order("config_changes.created_at").
		Find(&changes).Error; err != nil {
		return nil, err
	}

	return changes, nil
}

type ConfigLocationInfo struct {
	Count   int            `json:"count"`
	Type    sql.NullString `json:"type"`
	Region  sql.NullString `json:"region"`
	Account sql.NullString `json:"account"`
}

func GetApplicationLocations(ctx context.Context, environments map[string][]v1.ApplicationEnvironment) ([]api.ApplicationLocation, error) {
	var locations []api.ApplicationLocation
	for env, selectors := range environments {
		for _, purposeSelector := range selectors {
			selectColumns := []string{
				"tags->>'region' as region",
				"COALESCE(tags->>'account-name', tags->>'project') as account",
				"MAX(type) as type",
				"COUNT(*) as count",
			}

			clauses := []clause.Expression{
				// NOTE: We are targetting AWS and GCP config items by only matching configs
				// that have
				// tags.region + tags.account-name for aws OR
				// tags.project for GCP (gcp resources can be empty region)
				clause.Expr{
					SQL: "(tags->>'region' IS NOT NULL AND tags->>'account-name' IS NOT NULL) OR tags->>'project' IS NOT NULL",
				},
				clause.GroupBy{
					Columns: []clause.Column{
						{Name: "region"},
						{Name: "account"},
					},
				},
			}
			response, err := query.QueryTableColumnsWithResourceSelectors[ConfigLocationInfo](ctx, "config_items", selectColumns, -1, clauses, purposeSelector.ResourceSelector)
			if err != nil {
				return nil, err
			}

			seen := make(map[string]struct{})

			for _, row := range response {
				provider := configTypeToProvider(row.Type.String)
				key := fmt.Sprintf("%s-%s", row.Region.String, provider)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}

				location := api.ApplicationLocation{
					Name:          env,
					Account:       row.Account.String,
					Purpose:       purposeSelector.Purpose,
					Type:          "cloud",
					Region:        row.Region.String,
					Provider:      provider,
					ResourceCount: row.Count,
				}

				locations = append(locations, location)
			}
		}
	}

	return locations, nil
}

func configTypeToProvider(configType string) string {
	splits := strings.Split(configType, "::")
	return splits[0]
}

// GetChangesForUIRef queries config_changes using the filters from a ChangesUIFilters spec.
func GetChangesForUIRef(ctx context.Context, filters *api.ChangesUIFilters) ([]api.ApplicationChange, error) {
	if filters == nil {
		filters = &api.ChangesUIFilters{}
	}

	q := ctx.DB().
		Model(&models.ConfigChange{}).
		Select("config_changes.id, config_changes.created_at, config_changes.change_type, config_changes.summary, config_changes.source, config_changes.severity, config_changes.created_by").
		Order("config_changes.created_at DESC")

	if filters.ChangeType != "" {
		included, excluded := parseIncludeExcludeList(filters.ChangeType)
		if len(included) > 0 {
			q = q.Where("config_changes.change_type IN (?)", included)
		}
		if len(excluded) > 0 {
			q = q.Where("config_changes.change_type NOT IN (?)", excluded)
		}
	}

	if filters.Severity != "" {
		q = q.Where("config_changes.severity = ?", filters.Severity)
	}

	if filters.Source != "" {
		included, excluded := parseIncludeExcludeList(filters.Source)
		if len(included) > 0 {
			q = q.Where("config_changes.source IN (?)", included)
		}
		if len(excluded) > 0 {
			q = q.Where("config_changes.source NOT IN (?)", excluded)
		}
	}

	if filters.From != "" {
		if d, err := time.ParseDuration(filters.From); err == nil {
			q = q.Where("config_changes.created_at >= ?", time.Now().Add(-d))
		}
	}

	if filters.To != "" {
		if d, err := time.ParseDuration(filters.To); err == nil {
			q = q.Where("config_changes.created_at <= ?", time.Now().Add(-d))
		}
	}

	if filters.ConfigTypes != "" {
		included, excluded := parseIncludeExcludeList(filters.ConfigTypes)
		q = q.Joins("LEFT JOIN config_items ON config_items.id = config_changes.config_id").
			Where("(config_items.id IS NULL OR config_items.deleted_at IS NULL)")
		if len(included) > 0 {
			q = q.Where("config_items.type IN (?)", included)
		}
		if len(excluded) > 0 {
			q = q.Where("config_items.type NOT IN (?)", excluded)
		}
	}

	type changeRow struct {
		ID        uuid.UUID `gorm:"column:id"`
		CreatedAt time.Time `gorm:"column:created_at"`
		ChangeType string   `gorm:"column:change_type"`
		Summary   string    `gorm:"column:summary"`
		Source    string    `gorm:"column:source"`
		Severity  string    `gorm:"column:severity"`
		CreatedBy *string   `gorm:"column:created_by"`
	}

	var rows []changeRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to scan config changes")
	}

	changes := make([]api.ApplicationChange, len(rows))
	for i, r := range rows {
		changes[i] = api.ApplicationChange{
			ID:          r.ID.String(),
			Date:        r.CreatedAt,
			CreatedAt:   r.CreatedAt,
			ChangeType:  r.ChangeType,
			Source:      r.Source,
			CreatedBy:   lo.FromPtr(r.CreatedBy),
			Description: r.Summary,
			Status:      r.Severity,
		}
	}

	return changes, nil
}

// GetConfigsForUIRef queries config_items using the filters from a ConfigsUIFilters spec.
func GetConfigsForUIRef(ctx context.Context, filters *api.ConfigsUIFilters) ([]api.ApplicationConfigItem, error) {
	if filters == nil {
		filters = &api.ConfigsUIFilters{}
	}

	q := ctx.DB().
		Model(&models.ConfigItem{}).
		Select("id, name, type, status, health, labels").
		Where("deleted_at IS NULL").
		Order("name ASC")

	if filters.ConfigType != "" {
		q = q.Where("type = ?", filters.ConfigType)
	}

	if filters.Status != "" {
		included, excluded := parseIncludeExcludeList(filters.Status)
		if len(included) > 0 {
			q = q.Where("status IN (?)", included)
		}
		if len(excluded) > 0 {
			q = q.Where("status NOT IN (?)", excluded)
		}
	}

	if filters.Health != "" {
		included, excluded := parseIncludeExcludeList(filters.Health)
		if len(included) > 0 {
			q = q.Where("health IN (?)", included)
		}
		if len(excluded) > 0 {
			q = q.Where("health NOT IN (?)", excluded)
		}
	}

	if filters.Search != "" {
		q = q.Where("name ILIKE ?", "%"+filters.Search+"%")
	}

	type configRow struct {
		ID     uuid.UUID       `gorm:"column:id"`
		Name   *string         `gorm:"column:name"`
		Type   *string         `gorm:"column:type"`
		Status *string         `gorm:"column:status"`
		Health *string         `gorm:"column:health"`
		Labels json.RawMessage `gorm:"column:labels"`
	}

	var rows []configRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to scan config items")
	}

	configs := make([]api.ApplicationConfigItem, len(rows))
	for i, r := range rows {
		var labels map[string]string
		if len(r.Labels) > 0 {
			_ = json.Unmarshal(r.Labels, &labels)
		}
		configs[i] = api.ApplicationConfigItem{
			ID:     r.ID.String(),
			Name:   lo.FromPtr(r.Name),
			Type:   lo.FromPtr(r.Type),
			Status: lo.FromPtr(r.Status),
			Health: lo.FromPtr(r.Health),
			Labels: labels,
		}
	}

	return configs, nil
}

// parseIncludeExcludeList splits a comma-separated list into included and excluded values.
// Entries prefixed with "-" are excluded; all others are included.
func parseIncludeExcludeList(s string) (included, excluded []string) {
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if val, ok := strings.CutPrefix(part, "-"); ok {
			val = strings.TrimSpace(val)
			if val != "" {
				excluded = append(excluded, val)
			}
		} else {
			included = append(included, part)
		}
	}
	return
}
