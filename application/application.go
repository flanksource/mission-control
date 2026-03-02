package application

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/views"
)

var (
	// List of backup statuses we show on the application UI
	backupChangeTypes = []string{
		"BackupCompleted",
		"BackupEnqueued",
		"BackupFailed",
		"BackupRunning",
		"BackupStarted",
		"BackupSuccessful",
	}

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

		users, err := db.GetDistinctUserRoleFromConfigAccess(ctx, configs)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find config accesses: %w", err)
		}

		response.AccessControl.Users = users
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

	if selectors := app.AllSelectors(); len(selectors) > 0 {
		configIDs, err := query.FindConfigIDsByResourceSelector(ctx, -1, selectors...)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to find locations: %w", err)
		}

		var analyses []models.ConfigAnalysis
		if err := ctx.DB().Where("config_id IN ?", lo.Uniq(configIDs)).Find(&analyses).Error; err != nil {
			return nil, ctx.Oops().Errorf("failed to find analyses: %w", err)
		}

		for _, analysis := range analyses {
			response.Findings = append(response.Findings, api.ApplicationFinding{
				ID:           analysis.ID.String(),
				Type:         string(analysis.AnalysisType),
				Severity:     string(analysis.Severity),
				Title:        analysis.Summary,
				Description:  analysis.Message,
				Date:         lo.FromPtr(analysis.FirstObserved),
				LastObserved: lo.FromPtr(analysis.LastObserved),
				Status:       analysis.Status,
			})
		}
	}

	for _, section := range app.Spec.Sections {
		appSection, err := buildSection(ctx, section)
		if err != nil {
			return nil, ctx.Oops().Errorf("failed to build section %q: %w", section.Title, err)
		}
		response.Sections = append(response.Sections, appSection)
	}

	return &response, nil
}

func buildSection(ctx context.Context, section api.ViewSection) (api.ApplicationSection, error) {
	appSection := api.ApplicationSection{
		Title: section.Title,
		Icon:  section.Icon,
	}

	if section.ViewRef != nil {
		appSection.Type = api.SectionTypeView
		viewResult, err := views.ReadOrPopulateViewTable(ctx, section.ViewRef.Namespace, section.ViewRef.Name)
		if err != nil {
			return appSection, ctx.Oops().Errorf("failed to read view table (%s/%s): %w", section.ViewRef.Namespace, section.ViewRef.Name, err)
		}
		data := viewResultToSectionData(viewResult)
		appSection.View = &data
		return appSection, nil
	}

	if section.UIRef == nil {
		return appSection, nil
	}

	if section.UIRef.Changes != nil {
		appSection.Type = api.SectionTypeChanges
		changes, err := db.GetChangesForUIRef(ctx, section.UIRef.Changes)
		if err != nil {
			return appSection, ctx.Oops().Errorf("failed to get changes for section %q: %w", section.Title, err)
		}
		if changes == nil {
			changes = []api.ApplicationChange{}
		}
		appSection.Changes = changes
		return appSection, nil
	}

	if section.UIRef.Configs != nil {
		appSection.Type = api.SectionTypeConfigs
		configs, err := db.GetConfigsForUIRef(ctx, section.UIRef.Configs)
		if err != nil {
			return appSection, ctx.Oops().Errorf("failed to get configs for section %q: %w", section.Title, err)
		}
		if configs == nil {
			configs = []api.ApplicationConfigItem{}
		}
		appSection.Configs = configs
		return appSection, nil
	}

	return appSection, nil
}

func viewResultToSectionData(r *api.ViewResult) api.ApplicationViewData {
	return api.ApplicationViewData{
		RefreshStatus:   r.RefreshStatus,
		LastRefreshedAt: r.LastRefreshedAt,
		Columns:         r.Columns,
		Rows:            r.Rows,
		Panels:          r.Panels,
		Variables:       r.Variables,
		ColumnOptions:   r.ColumnOptions,
	}
}

func PersistApplication(ctx context.Context, app *v1.Application) error {
	if err := db.PersistApplicationFromCRD(ctx, app); err != nil {
		return err
	}

	return syncApplication(ctx, app)
}
