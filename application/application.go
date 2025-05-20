package application

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

func buildApplication(ctx context.Context, app *v1.Application) (*api.Application, error) {
	response := api.Application{
		ApplicationDetail: api.ApplicationDetail{
			ID:          app.GetID().String(),
			Type:        app.Spec.Type,
			Namespace:   app.Namespace,
			Name:        app.Name,
			Description: app.Spec.Description,
			CreatedAt:   app.CreationTimestamp.Time,
		},
	}

	mapping := app.Spec.Mapping
	if len(mapping.Logins) > 0 {
		configs, err := query.FindConfigIDsByResourceSelector(ctx, -1, mapping.Logins...)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find login IDs: %w", err)
		}

		configAccesses, err := query.FindConfigAccessByConfigIDs(ctx, configs)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find config accesses: %w", err)
		}

		for _, ca := range configAccesses {
			response.AccessControl.Users = append(response.AccessControl.Users, api.UserAndRole{
				Name:             ca.User,
				Email:            ca.Email,
				Role:             "",    // TODO:
				AuthType:         "sso", // TODO:
				CreatedAt:        ca.CreatedAt,
				LastLogin:        ca.LastSignedInAt,
				LastAccessReview: ca.LastReviewedAt,
			})
		}
	}

	// TODO: Remove this
	injectDummyData(&response)

	return &response, nil
}

func PersistApplication(ctx context.Context, app *v1.Application) error {
	if err := db.PersistApplicationFromCRD(ctx, app); err != nil {
		return err
	}

	job := SyncApplicationScrapeConfigs(ctx)
	job.Run()
	return nil
}

func injectDummyData(app *api.Application) {
	app.AccessControl.Authentication = []api.AuthMethod{
		{
			Name: "Active Directory",
			Type: "LDAP",
			MFA: &api.AuthMethodMFA{
				Type:     "Mobile",
				Enforced: "On login",
			},
		},
		{
			Name: "SAP SSO",
			Type: "OAUTH",
			MFA: &api.AuthMethodMFA{
				Type:     "Mobile",
				Enforced: "On login, and every 2w",
			},
		},
	}

	app.Changes = []api.ApplicationChange{
		{
			ID:          "1",
			Date:        parseDate("2024-05-30"),
			User:        "John Doe",
			Description: "Updated user permissions for finance module",
			Status:      "completed",
		},
		{
			ID:          "2",
			Date:        parseDate("2024-05-28"),
			User:        "Jane Smith",
			Description: "Deployed v2.4.0 to production",
			Status:      "completed",
		},
		{
			ID:          "3",
			Date:        parseDate("2024-05-25"),
			User:        "Michael Brown",
			Description: "Modified database schema for customers table",
			Status:      "failed",
		},
	}

	app.Incidents = []api.ApplicationIncident{
		{
			ID:           "1",
			Date:         parseDate("2024-05-28"),
			Severity:     "high",
			Description:  "API service outage affecting customer portal",
			Status:       "resolved",
			ResolvedDate: lo.ToPtr(parseDate("2024-05-28")),
		},
		{
			ID:           "2",
			Date:         parseDate("2024-05-20"),
			Severity:     "medium",
			Description:  "Elevated error rates in payment processing",
			Status:       "resolved",
			ResolvedDate: lo.ToPtr(parseDate("2024-05-21")),
		},
		{
			ID:           "3",
			Date:         parseDate("2024-05-15"),
			Severity:     "low",
			Description:  "Slow response times in reporting module",
			Status:       "resolved",
			ResolvedDate: lo.ToPtr(parseDate("2024-05-16")),
		},
		{
			ID:          "4",
			Date:        parseDate("2024-05-15"),
			Severity:    "low",
			Description: "Failed to connect to database",
			Status:      "investigating",
		},
	}
}

func parseDate(dateStr string) time.Time {
	t, _ := time.Parse("2006-01-02", dateStr)
	return t
}
