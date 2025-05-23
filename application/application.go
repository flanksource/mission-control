package application

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"

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
				CreatedAt:        ca.CreatedAt,
				LastLogin:        ca.LastSignedInAt,
				LastAccessReview: ca.LastReviewedAt,
			})
		}
	}

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
