package application

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	uuidV5 "github.com/gofrs/uuid/v5"
	"github.com/google/uuid"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

// A minimal copy of azure scraper from config-db
type AzureScraper struct {
	ConnectionName     string                   `yaml:"connection,omitempty" json:"connection,omitempty"`
	SubscriptionID     string                   `yaml:"subscriptionID" json:"subscriptionID"`
	ClientID           types.EnvVar             `yaml:"clientID,omitempty" json:"clientID,omitempty"`
	ClientSecret       types.EnvVar             `yaml:"clientSecret,omitempty" json:"clientSecret,omitempty"`
	TenantID           string                   `yaml:"tenantID,omitempty" json:"tenantID,omitempty"`
	Include            []string                 `yaml:"include,omitempty" json:"include,omitempty"`
	AppRoleAssignments []types.ResourceSelector `yaml:"appRoleAssignments,omitempty" json:"appRoleAssignments,omitempty"`
}

type ScraperSpec struct {
	Schedule string         `json:"schedule,omitempty"`
	Azure    []AzureScraper `yaml:"azure,omitempty" json:"azure,omitempty"`
}

// generateConfigScraper generates a config scraper for the configs targetted by the login selector
func generateConfigScraper(ctx context.Context, app *v1.Application) error {
	loginSelector := app.Spec.Mapping.Logins
	if len(loginSelector) == 0 {
		return nil
	}

	azureScrapeConfigs, err := GetAllAzureScrapeConfigs(ctx)
	if err != nil {
		return nil
	}

	for _, scrapeConfig := range azureScrapeConfigs {
		var spec ScraperSpec
		if err := json.Unmarshal([]byte(scrapeConfig.Spec), &spec); err != nil {
			return ctx.Oops().Errorf("failed to unmarshal scrape config %s: %v", scrapeConfig.ID, err)
		}

		spec.Azure[0].Include = []string{"appRoleAssignments"}
		spec.Azure[0].AppRoleAssignments = lo.Map(loginSelector, func(selector types.ResourceSelector, _ int) types.ResourceSelector {
			selector.Scope = scrapeConfig.ID.String()
			return selector
		})

		specJSON, err := json.Marshal(spec)
		if err != nil {
			return err
		}

		// generate a deterministic id for the scraper based on the application id
		id := uuid.UUID(uuidV5.NewV5(uuidV5.UUID(scrapeConfig.ID), string(app.UID)))
		scraper := &models.ConfigScraper{
			ID:            id,
			Namespace:     scrapeConfig.Namespace,
			Name:          fmt.Sprintf("%s/%s-%s-app-generated", scrapeConfig.Namespace, app.Name, scrapeConfig.Name),
			Description:   scrapeConfig.Description,
			Spec:          string(specJSON),
			Source:        models.SourceApplicationCRD,
			ApplicationID: lo.ToPtr(app.GetID()),
		}
		if err := ctx.DB().Save(scraper).Error; err != nil {
			return err
		}
	}

	return nil
}

func linkToConfigs(ctx context.Context, app *v1.Application) error {
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

func GetAllAzureScrapeConfigs(ctx context.Context) ([]models.ConfigScraper, error) {
	var configs []models.ConfigScraper
	if err := ctx.DB().Where("deleted_at IS NULL").
		Where("spec->>'azure' IS NOT NULL").
		Where("application_id IS NULL").
		Find(&configs).Error; err != nil {
		return nil, err
	}

	return configs, nil
}
