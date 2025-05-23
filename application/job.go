package application

import (
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	uuidV5 "github.com/gofrs/uuid/v5"
	"github.com/google/uuid"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

func linkToConfigs(ctx context.Context, app *v1.Application) error {
	// Ensure the application config item exists before we form the relationships
	var application models.ConfigItem
	if err := ctx.DB().Where("id = ?", app.GetID()).Find(&application).Error; err != nil {
		return err
	} else if application.ID == uuid.Nil {
		return nil
	}

	configIDs, err := query.FindConfigIDsByResourceSelector(ctx, -1, app.Spec.Mapping.Logins...)
	if err != nil {
		return err
	}

	relationships := make([]models.ConfigRelationship, len(configIDs))
	for i, configID := range configIDs {
		relationships[i] = models.ConfigRelationship{
			ConfigID:  string(app.GetUID()),
			RelatedID: configID.String(),
			Relation:  "ApplicationLogin",
		}
	}

	if err := ctx.DB().Save(relationships).Error; err != nil {
		return err
	}

	return nil
}

func SyncApplications(sc context.Context) *job.Job {
	return &job.Job{
		Name:          "SyncApplications",
		Context:       sc,
		Schedule:      "@every 1h",
		Singleton:     true,
		JitterDisable: true,
		JobHistory:    true,
		Retention:     job.RetentionFew,
		RunNow:        true,
		Fn: func(jr job.JobRuntime) error {
			applications, err := db.GetAllApplications(sc)
			if err != nil {
				return sc.Oops().Errorf("failed to get applications: %w", err)
			}

			for _, application := range applications {
				app, err := v1.ApplicationFromModel(application)
				if err != nil {
					return sc.Oops().Errorf("failed to get application: %w", err)
				}

				if err := syncApplication(sc, app); err != nil {
					return sc.Oops().Errorf("failed to sync application (%s/%s): %w", app.Namespace, app.Name, err)
				}

				jr.History.IncrSuccess()
			}

			applicationIDs := lo.Map(applications, func(app models.Application, _ int) uuid.UUID {
				return app.ID
			})

			return cleanupStaleScrapers(sc, applicationIDs)
		},
	}
}

// syncApplication processes aplication mappings
func syncApplication(ctx context.Context, app *v1.Application) error {
	if err := generateConfigScraper(ctx, app); err != nil {
		return ctx.Oops().Errorf("failed to generate config scraper: %w", err)
	}

	if err := linkToConfigs(ctx, app); err != nil {
		return ctx.Oops().Errorf("failed to link to configs: %w", err)
	}

	if err := generateCustomRoles(ctx, app.GetID(), app); err != nil {
		return ctx.Oops().Errorf("failed to generate custom roles: %w", err)
	}

	return nil
}

// Generate new custom roles & config accesses for those roles
func generateCustomRoles(ctx context.Context, applicationID uuid.UUID, app *v1.Application) error {
	for _, role := range app.Spec.Mapping.Roles {
		roleID := uuid.UUID(uuidV5.NewV5(uuidV5.NamespaceDNS, fmt.Sprintf("%s-%s", app.UID, role.Role)))

		externalRole := models.ExternalRole{
			ID:            roleID,
			Name:          role.Role,
			ApplicationID: &applicationID,
			Description:   "Custom Mapped Role",
		}

		if err := ctx.DB().Save(&externalRole).Error; err != nil {
			return ctx.Oops().Errorf("failed to persist custom external role: %w", err)
		}

		configIDs, err := query.FindConfigIDsByResourceSelector(ctx, -1, role.ResourceSelector)
		if err != nil {
			return ctx.Oops().Errorf("failed to find login IDs: %w", err)
		}

		if len(configIDs) == 0 {
			continue
		}

		roleConfigAccesses := lo.Map(configIDs, func(configID uuid.UUID, _ int) models.ConfigAccess {
			return models.ConfigAccess{
				ConfigID:       configID,
				ExternalRoleID: lo.ToPtr(roleID),
				ApplicationID:  &applicationID,
			}
		})

		if err := ctx.DB().Save(roleConfigAccesses).Error; err != nil {
			return ctx.Oops().Errorf("failed to persist config accesses: %w", err)
		}
	}

	return nil
}

func cleanupStaleScrapers(ctx context.Context, activeApplicationIDs []uuid.UUID) error {
	tx := ctx.DB().Model(&models.ConfigScraper{}).
		Where("deleted_at IS NULL").
		Where("source = ?", models.SourceApplicationCRD).
		Where("application_id IS NOT NULL").
		Where("application_id NOT IN (?)", activeApplicationIDs).
		Update("deleted_at", duty.Now())
	if tx.Error != nil {
		return tx.Error
	}

	if tx.RowsAffected > 0 {
		ctx.Infof("deleted %d stale application generated config scrapers", tx.RowsAffected)
	}

	return nil
}
