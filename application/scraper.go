package application

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
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
		id := uuid.UUID(uuidV5.NewV5(uuidV5.Nil, string(app.UID)))
		scraper := &models.ConfigScraper{
			ID:            id,
			Namespace:     scrapeConfig.Namespace,
			Name:          fmt.Sprintf("%s/%s-app-generated", scrapeConfig.Namespace, app.Name),
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

func SyncApplicationScrapeConfigs(sc context.Context) *job.Job {
	return &job.Job{
		Name:          "ApplicationConfigScraperSync",
		Context:       sc,
		Schedule:      "@every 10m",
		Singleton:     true,
		JitterDisable: true,
		JobHistory:    true,
		Retention:     job.RetentionFew,
		RunNow:        true,
		Fn: func(jr job.JobRuntime) error {
			applications, err := db.GetAllApplications(sc)
			if err != nil {
				return err
			}

			for _, application := range applications {
				app, err := v1.ApplicationFromModel(application)
				if err != nil {
					return err
				}

				if err := generateConfigScraper(sc, app); err != nil {
					return err
				}
			}

			applicationIDs := lo.Map(applications, func(app models.Application, _ int) uuid.UUID {
				return app.ID
			})

			return cleanupStaleScrapers(sc, applicationIDs)
		},
	}
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
		Find(&configs).Error; err != nil {
		return nil, err
	}

	return configs, nil
}
