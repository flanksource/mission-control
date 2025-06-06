package application

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	uuidV5 "github.com/gofrs/uuid/v5"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm/clause"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// A minimal copy of azure scraper from config-db.
//
// It should have only the connection details and just the field that we want to set
// i.e. Entra.
type AzureScraper struct {
	ConnectionName string       `yaml:"connection,omitempty" json:"connection,omitempty"`
	SubscriptionID string       `yaml:"subscriptionID" json:"subscriptionID"`
	ClientID       types.EnvVar `yaml:"clientID" json:"clientID"`
	ClientSecret   types.EnvVar `yaml:"clientSecret" json:"clientSecret"`
	TenantID       string       `yaml:"tenantID,omitempty" json:"tenantID,omitempty"`
	Entra          *Entra       `yaml:"entra,omitempty" json:"entra,omitempty"`
}

type Entra struct {
	Users              []types.ResourceSelector `yaml:"users,omitempty" json:"users,omitempty"`
	Groups             []types.ResourceSelector `yaml:"groups,omitempty" json:"groups,omitempty"`
	AppRegistrations   []types.ResourceSelector `yaml:"appRegistrations,omitempty" json:"appRegistrations,omitempty"`
	EnterpriseApps     []types.ResourceSelector `yaml:"enterpriseApps,omitempty" json:"enterpriseApps,omitempty"`
	AppRoleAssignments []types.ResourceSelector `yaml:"appRoleAssignments,omitempty" json:"appRoleAssignments,omitempty"`
}

// A minimal copy of GCP scraper from config-db.
//
// It should have only the connection details and just the field that we want to set
// i.e. AuditLogs.
type GCPScraper struct {
	connection.GCPConnection `json:",inline"`
	ConnectionName           string       `yaml:"connection,omitempty" json:"connection,omitempty"`
	Project                  string       `yaml:"project" json:"project"`
	AuditLogs                GCPAuditLogs `yaml:"auditLogs" json:"auditLogs"`
}

type GCPAuditLogs struct {
	Enabled      bool     `json:"enabled,omitempty"`
	IncludeTypes []string `json:"includeTypes,omitempty"`
	ExcludeTypes []string `json:"excludeTypes,omitempty"`

	// The lookback period for audit logs.
	// Default: 7d
	MaxDuration string `json:"maxDuration,omitempty"`
}

type ScraperSpec struct {
	Schedule string         `json:"schedule,omitempty"`
	Azure    []AzureScraper `yaml:"azure,omitempty" json:"azure,omitempty"`
	GCP      []GCPScraper   `yaml:"gcp,omitempty" json:"gcp,omitempty"`
}

// generateConfigScraper generates a config scraper for the configs targetted by the login selector
func generateConfigScraper(ctx context.Context, app *v1.Application) error {
	loginSelector := app.Spec.Mapping.Logins
	if len(loginSelector) == 0 {
		return nil
	}

	// Collect all scrapers and their associated selectors
	scraperSelectors := make(map[uuid.UUID][]types.ResourceSelector)
	scraperMap := make(map[uuid.UUID]*models.ConfigScraper)

	// For each resource selector, find the configs and their scrapers
	for _, selector := range loginSelector {
		// Get scrapers for configs targeted by this selector
		scrapers, err := getScrapersForResourceSelector(ctx, selector)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to get scrapers for selector")
		}

		for _, scraper := range scrapers {
			// Add selector to this scraper
			scraperSelectors[scraper.ID] = append(scraperSelectors[scraper.ID], selector)
			scraperMap[scraper.ID] = &scraper
		}
	}

	ctx.Logger.V(1).Infof("found %d unique scrapers for %d selectors", len(scraperMap), len(loginSelector))

	// Now create one generated scraper per unique scraper, combining all selectors
	for scraperID, selectors := range scraperSelectors {
		scraper := scraperMap[scraperID]
		var spec ScraperSpec
		if err := json.Unmarshal([]byte(scraper.Spec), &spec); err != nil {
			return ctx.Oops().Wrapf(err, "failed to unmarshal scrape config %s", scraperID)
		}

		if len(spec.Azure) > 0 {
			appRoleSelectors := lo.Map(selectors, func(selector types.ResourceSelector, _ int) types.ResourceSelector {
				selector.Scope = scraperID.String()
				return selector
			})

			// Keep the remaining fields the same. Just modify the Entra config.
			spec.Azure[0].Entra = &Entra{
				AppRoleAssignments: appRoleSelectors,
			}
		}

		if len(spec.GCP) > 0 {
			// Keep the remaining fields the same. Just modify the audit logs.
			spec.GCP[0].AuditLogs = GCPAuditLogs{
				Enabled: true,
			}
		}

		specJSON, err := json.Marshal(spec)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to marshal scrape config %s", scraperID)
		}

		// generate a deterministic id for the scraper based on the application id
		id := uuid.UUID(uuidV5.NewV5(uuidV5.UUID(scraperID), string(app.UID)))
		generatedScraper := &models.ConfigScraper{
			ID:            id,
			Namespace:     scraper.Namespace,
			Name:          fmt.Sprintf("%s/%s-%s-app-generated", scraper.Namespace, app.Name, scraper.Name),
			Description:   scraper.Description,
			Spec:          string(specJSON),
			Source:        models.SourceApplicationCRD,
			ApplicationID: lo.ToPtr(app.GetID()),
		}
		if err := ctx.DB().Save(generatedScraper).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to save scrape config %s", scraperID)
		}
	}

	return nil
}

// getScrapersForResourceSelector finds scrapers for configs targeted by the resource selector
func getScrapersForResourceSelector(ctx context.Context, selector types.ResourceSelector) ([]models.ConfigScraper, error) {
	// First, get config items that match the resource selector
	configColumns := []string{
		"scraper_id",
	}
	configClauses := []clause.Expression{
		clause.Expr{SQL: "scraper_id IS NOT NULL"},
		clause.GroupBy{
			Columns: []clause.Column{
				{Name: "scraper_id"},
			},
		},
	}

	type ConfigScraperRef struct {
		ScraperID uuid.UUID `json:"scraper_id"`
	}

	configRefs, err := query.QueryTableColumnsWithResourceSelectors[ConfigScraperRef](ctx, "config_items", configColumns, -1, configClauses, selector)
	if err != nil {
		return nil, err
	}

	if len(configRefs) == 0 {
		return []models.ConfigScraper{}, nil
	}

	// Extract scraper IDs
	scraperIDs := lo.Map(configRefs, func(ref ConfigScraperRef, _ int) uuid.UUID {
		return ref.ScraperID
	})

	var scrapers []models.ConfigScraper
	if err := ctx.DB().
		Where("deleted_at IS NULL").
		Where("application_id IS NULL"). // we want to ignore generated scrapers
		Where("id IN (?)", scraperIDs).
		Find(&scrapers).Error; err != nil {
		return nil, err
	}

	return scrapers, nil
}
