package application

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	uuidV5 "github.com/gofrs/uuid/v5"
	"github.com/google/uuid"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// A minimal copy of azure scraper from config-db
type AzureScraper struct {
	ConnectionName string       `yaml:"connection,omitempty" json:"connection,omitempty"`
	SubscriptionID string       `yaml:"subscriptionID" json:"subscriptionID"`
	ClientID       types.EnvVar `yaml:"clientID,omitempty" json:"clientID,omitempty"`
	ClientSecret   types.EnvVar `yaml:"clientSecret,omitempty" json:"clientSecret,omitempty"`
	TenantID       string       `yaml:"tenantID,omitempty" json:"tenantID,omitempty"`
	Include        []string     `yaml:"include,omitempty" json:"include,omitempty"`
	Entra          *Entra       `yaml:"entra,omitempty" json:"entra,omitempty"`
}

type Entra struct {
	Users              []types.ResourceSelector `yaml:"users,omitempty" json:"users,omitempty"`
	Groups             []types.ResourceSelector `yaml:"groups,omitempty" json:"groups,omitempty"`
	AppRegistrations   []types.ResourceSelector `yaml:"appRegistrations,omitempty" json:"appRegistrations,omitempty"`
	EnterpriseApps     []types.ResourceSelector `yaml:"enterpriseApps,omitempty" json:"enterpriseApps,omitempty"`
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
		return ctx.Oops().Wrapf(err, "failed to get all azure scrape configs")
	}

	ctx.Infof("found %d azure scrape configs", len(azureScrapeConfigs))

	for _, scrapeConfig := range azureScrapeConfigs {
		var spec ScraperSpec
		if err := json.Unmarshal([]byte(scrapeConfig.Spec), &spec); err != nil {
			return ctx.Oops().Wrapf(err, "failed to unmarshal scrape config %s", scrapeConfig.ID)
		}

		spec.Azure[0].Include = []string{"entra", "appRoleAssignments"}
		spec.Azure[0].Entra = &Entra{
			AppRoleAssignments: lo.Map(loginSelector, func(selector types.ResourceSelector, _ int) types.ResourceSelector {
				selector.Scope = scrapeConfig.ID.String()
				return selector
			}),
		}

		specJSON, err := json.Marshal(spec)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to marshal scrape config %s", scrapeConfig.ID)
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
			return ctx.Oops().Wrapf(err, "failed to save scrape config %s", scrapeConfig.ID)
		}
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
