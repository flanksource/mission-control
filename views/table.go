package views

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"go.opentelemetry.io/otel/trace"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

// ReadOrPopulateViewTable reads view data from the view's table.
// If the table does not exist, it will be created and the view will be populated.
func ReadOrPopulateViewTable(ctx context.Context, namespace, name string) (*api.ViewResult, error) {
	view, err := db.GetView(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get view: %w", err)
	}

	tableName := view.TableName()

	if !ctx.DB().Migrator().HasTable(tableName) {
		if err := db.CreateViewTable(ctx, view); err != nil {
			return nil, fmt.Errorf("failed to create view table: %w", err)
		}

		result, err := PopulateView(ctx, view)
		if err != nil {
			return nil, fmt.Errorf("failed to run view: %w", err)
		}

		return result, nil
	}

	rows, err := db.ReadViewTable(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to read view table: %w", err)
	}

	var panelResult models.ViewPanel
	if err := ctx.DB().Where("view_id = ?", view.GetUID()).Find(&panelResult).Error; err != nil {
		return nil, fmt.Errorf("failed to find panel results: %w", err)
	}

	var finalPanelResults []api.PanelResult
	if len(panelResult.Results) > 0 {
		if err := json.Unmarshal(panelResult.Results, &finalPanelResults); err != nil {
			return nil, fmt.Errorf("failed to unmarshal panel results: %w", err)
		}
	}

	return &api.ViewResult{
		Columns: view.Spec.Columns,
		Rows:    rows,
		Panels:  finalPanelResults,
	}, nil
}

// PopulateView runs the view queries and saves to the view table.
func PopulateView(ctx context.Context, view *v1.View) (*api.ViewResult, error) {
	result, err := Run(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to run view: %w", err)
	}

	// The following queries first remove existing records and then save them.
	// So they are done in a single transaction.
	err = ctx.Transaction(func(ctx context.Context, span trace.Span) error {
		tableName := view.TableName()
		if !ctx.DB().Migrator().HasTable(tableName) {
			if err := db.CreateViewTable(ctx, view); err != nil {
				return fmt.Errorf("failed to create view table: %w", err)
			}
		}

		// View rows are saved into their own dedicated table.
		if err := db.InsertViewRows(ctx, tableName, result.Columns, result.Rows); err != nil {
			return fmt.Errorf("failed to insert view rows: %w", err)
		}

		// All the panel results from all the views are saved into the same table
		if len(result.Panels) > 0 {
			uid, err := view.GetUUID()
			if err != nil {
				return fmt.Errorf("failed to get view uid: %w", err)
			}

			if err := db.InsertPanelResults(ctx, uid, result.Panels); err != nil {
				return fmt.Errorf("failed to insert view panels: %w", err)
			}
		}

		return nil
	})

	return result, err
}
