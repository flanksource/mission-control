package views

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	pkgView "github.com/flanksource/duty/view"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

// A hard limit on the refresh timeout.
const defaultMaxRefreshTimeout = time.Minute

var (
	// refreshGroup deduplicates concurrent view refresh operations
	refreshGroup singleflight.Group
)

// ViewOption is a functional option for configuring view operations
type ViewOption func(*viewConfig)

// viewConfig holds configuration options for view operations
type viewConfig struct {
	maxAge         *time.Duration
	refreshTimeout *time.Duration
}

// WithMaxAge sets the maximum age for cached view data
func WithMaxAge(maxAge time.Duration) ViewOption {
	return func(c *viewConfig) {
		c.maxAge = &maxAge
	}
}

// WithRefreshTimeout sets the timeout for view refresh operations
func WithRefreshTimeout(timeout time.Duration) ViewOption {
	return func(c *viewConfig) {
		c.refreshTimeout = &timeout
	}
}

// ReadOrPopulateViewTable reads view data from the view's table with cache control.
// If the table does not exist, it will be created and the view will be populated.
// If the cache has expired based on maxAge, the view will be refreshed with timeout handling.
func ReadOrPopulateViewTable(ctx context.Context, namespace, name string, opts ...ViewOption) (*api.ViewResult, error) {
	view, err := db.GetView(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get view: %w", err)
	} else if view == nil {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view %s/%s not found", namespace, name)
	}

	config := &viewConfig{}
	for _, opt := range opts {
		opt(config)
	}

	var headerMaxAge, headerRefreshTimeout time.Duration
	if config.maxAge != nil {
		headerMaxAge = *config.maxAge
	}
	if config.refreshTimeout != nil {
		headerRefreshTimeout = *config.refreshTimeout
	}

	cacheOptions, err := view.GetCacheOptions(headerMaxAge, headerRefreshTimeout)
	if err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "%s", err.Error())
	}

	tableName := view.TableName()
	tableExists := ctx.DB().Migrator().HasTable(tableName)
	cacheExpired := view.CacheExpired(cacheOptions.MaxAge)

	if tableExists && !cacheExpired {
		return readCachedViewData(ctx, view)
	}

	return handleViewRefresh(ctx, view, cacheOptions, tableExists)
}

// handleViewRefresh deduplicates concurrent view refresh operations using singleflight
func handleViewRefresh(ctx context.Context, view *v1.View, cacheOptions *v1.CacheOptions, tableExists bool) (*api.ViewResult, error) {
	done := make(chan struct{})
	var result *api.ViewResult
	var err error

	go func() {
		defer close(done)

		// Need to create a new context with a longer timeout.
		// The refresh needs to outlive the request context.
		newCtx, cancel := gocontext.WithTimeout(gocontext.Background(), ctx.Properties().Duration("view.refresh.max-timeout", defaultMaxRefreshTimeout))
		defer cancel()
		clonedCtx := ctx.Clone()
		clonedCtx.Context = newCtx
		refreshCtx := context.NewContext(clonedCtx).WithDB(ctx.DB(), ctx.Pool()).WithConnectionString(ctx.ConnectionString())
		if ctx.User() != nil {
			refreshCtx = refreshCtx.WithUser(ctx.User())
		}

		res, refreshErr, _ := refreshGroup.Do(string(view.GetUID()), func() (any, error) {
			return populateView(refreshCtx, view)
		})
		if refreshErr != nil {
			ctx.Errorf("failed to refresh view %s: %v", view.GetNamespacedName(), refreshErr)
			err = refreshErr
		} else {
			result = res.(*api.ViewResult)
		}
	}()

	select {
	case <-done:
		if err != nil {
			if tableExists {
				ctx.Logger.Errorf("failed to refresh view %s: %v", view.GetNamespacedName(), err)
				return readCachedViewData(ctx, view)
			}

			return nil, fmt.Errorf("failed to refresh view %s: %w", view.GetNamespacedName(), err)
		}

		return result, nil

	case <-time.After(cacheOptions.RefreshTimeout):
		if tableExists {
			ctx.Logger.Debugf("view %s refresh timeout reached. returning cached data", view.GetNamespacedName())
			return readCachedViewData(ctx, view)
		}

		return nil, fmt.Errorf("view %s refresh timeout reached. try again", view.GetNamespacedName())
	}
}

// readCachedViewData reads cached data from the view table
func readCachedViewData(ctx context.Context, view *v1.View) (*api.ViewResult, error) {
	columns := append(view.Spec.Columns, pkgView.ColumnDef{
		Name: pkgView.ReservedColumnAttributes,
		Type: pkgView.ColumnTypeAttributes,
	})

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

	result := &api.ViewResult{
		Columns: columns,
		Panels:  finalPanelResults,
	}

	if view.Status.LastRan != nil {
		result.LastRefreshedAt = view.Status.LastRan.Time
	}

	// Populate column options for multiselect filters
	columnOptions, err := getColumnOptions(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to get column options: %w", err)
	}
	result.ColumnOptions = columnOptions

	return result, nil
}

// populateView runs the view queries and saves to the view table.
func populateView(ctx context.Context, view *v1.View) (*api.ViewResult, error) {
	result, err := Run(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to run view: %w", err)
	}

	if view.HasTable() {
		err = ctx.Transaction(func(ctx context.Context, span trace.Span) error {
			if err := pkgView.CreateViewTable(ctx, view.TableName(), view.Spec.Columns); err != nil {
				return fmt.Errorf("failed to create view table: %w", err)
			}

			if err := persistViewData(ctx, view, result); err != nil {
				return err
			}

			if err := updateViewLastRan(ctx, string(view.GetUID())); err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return result, err
		}
	}

	result.Rows = []pkgView.Row{}
	result.LastRefreshedAt = time.Now()

	// Populate column options for multiselect filters
	columnOptions, err := getColumnOptions(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to get column options: %w", err)
	}
	result.ColumnOptions = columnOptions

	return result, nil
}

// persistViewData saves view rows and panel results to their respective tables
func persistViewData(ctx context.Context, view *v1.View, result *api.ViewResult) error {
	tableName := view.TableName()

	// Save view rows to the dedicated table
	if err := pkgView.InsertViewRows(ctx, tableName, result.Columns, result.Rows); err != nil {
		return fmt.Errorf("failed to insert view rows: %w", err)
	}

	// Save panel results if any exist
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
}

// updateViewLastRan updates the last_ran timestamp for the view
func updateViewLastRan(ctx context.Context, id string) error {
	if err := ctx.DB().Model(&models.View{}).
		Where("id = ?", id).
		Update("last_ran", duty.Now()).Error; err != nil {
		return fmt.Errorf("failed to update lastRan field: %w", err)
	}
	return nil
}

// getColumnOptions retrieves distinct values for columns with multiselect filters
func getColumnOptions(ctx context.Context, view *v1.View) (map[string][]string, error) {
	if !view.HasTable() {
		return nil, nil
	}

	columnOptions := make(map[string][]string)
	tableName := view.TableName()

	// Check if the table exists
	if !ctx.DB().Migrator().HasTable(tableName) {
		return columnOptions, nil
	}

	// Find columns with multiselect filters
	for _, column := range view.Spec.Columns {
		if column.Filter != nil && column.Filter.Type == pkgView.ColumnFilterTypeMultiSelect {
			var values []string
			
			// Query distinct values for this column, excluding null values
			if err := ctx.DB().Table(tableName).
				Distinct(column.Name).
				Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", column.Name, column.Name)).
				Pluck(column.Name, &values).Error; err != nil {
				return nil, fmt.Errorf("failed to get distinct values for column %s: %w", column.Name, err)
			}

			columnOptions[column.Name] = values
		}
	}

	return columnOptions, nil
}
