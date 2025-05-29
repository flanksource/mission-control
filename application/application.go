package application

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

var (
	backupChangeTypes        = []string{"BackupCompleted", "BackupStarted"}
	backupRestoreChangeTypes = []string{"BackupRestored", "RestoreCompleted"}
)

func buildApplication(ctx context.Context, app *v1.Application) (*api.Application, error) {
	response := api.Application{
		ApplicationDetail: api.ApplicationDetail{
			ID:          app.GetID().String(),
			Type:        app.Spec.Type,
			Namespace:   app.Namespace,
			Name:        app.Name,
			Description: app.Spec.Description,
			Properties:  app.Spec.Properties,
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

	if len(mapping.Datasources) > 0 {
		configIDs, err := query.FindConfigIDsByResourceSelector(ctx, -1, mapping.Datasources...)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find login IDs: %w", err)
		}

		backups, err := db.GetApplicationBackups(ctx, configIDs, backupChangeTypes)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find changes for backups: %w", err)
		}

		for _, change := range backups {
			response.Backups = append(response.Backups, api.ApplicationBackup{
				ID:       change.ID.String(),
				Database: change.Name,
				Type:     change.ConfigType,
				Source:   change.Source,
				Date:     change.CreatedAt,
				Size:     change.Size,
				Status:   change.Status,
			})
		}

		restores, err := db.GetApplicationRestores(ctx, configIDs, backupRestoreChangeTypes)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find changes for restores: %w", err)
		}

		for _, change := range restores {
			response.Restores = append(response.Restores, api.ApplicationBackupRestore{
				ID:       change.ID.String(),
				Database: change.Name,
				Date:     change.CreatedAt,
				Status:   change.Status,
			})
		}
	}

	if len(mapping.Environments) > 0 {
		locations, err := db.GetApplicationLocations(ctx, mapping.Environments)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find locations: %w", err)
		}

		response.Locations = locations
	}

	return &response, nil
}

func PersistApplication(ctx context.Context, app *v1.Application) error {
	if err := db.PersistApplicationFromCRD(ctx, app); err != nil {
		return err
	}

	return syncApplication(ctx, app)
}
