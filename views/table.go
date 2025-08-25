package views

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/hash"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
	"github.com/lib/pq"
	"github.com/samber/lo"
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
type ViewOption func(*requestOpt)

// requestOpt holds configuration options for view operations
type requestOpt struct {
	maxAge         *time.Duration
	refreshTimeout *time.Duration
	includeRows    bool
	variables      map[string]string
}

func (t requestOpt) Fingerprint() string {
	return hash.Sha256Hex(collections.SortedMap(t.variables))
}

func WithVariable(key, value string) ViewOption {
	return func(c *requestOpt) {
		if c.variables == nil {
			c.variables = make(map[string]string)
		}
		c.variables[key] = value
	}
}

// WithMaxAge sets the maximum age for cached view data
func WithMaxAge(maxAge time.Duration) ViewOption {
	return func(c *requestOpt) {
		c.maxAge = &maxAge
	}
}

// WithRefreshTimeout sets the timeout for view refresh operations
func WithRefreshTimeout(timeout time.Duration) ViewOption {
	return func(c *requestOpt) {
		c.refreshTimeout = &timeout
	}
}

// WithIncludeRows sets whether to include table rows in the response
func WithIncludeRows(include bool) ViewOption {
	return func(c *requestOpt) {
		c.includeRows = include
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

	request := &requestOpt{}
	for _, opt := range opts {
		opt(request)
	}

	var headerMaxAge, headerRefreshTimeout time.Duration
	if request.maxAge != nil {
		headerMaxAge = *request.maxAge
	}
	if request.refreshTimeout != nil {
		headerRefreshTimeout = *request.refreshTimeout
	}

	cacheOptions, err := view.GetCacheOptions(headerMaxAge, headerRefreshTimeout)
	if err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "%s", err.Error())
	}

	tableName := view.TableName()
	tableExists := ctx.DB().Migrator().HasTable(tableName)

	// Check cache expiration for the specific request fingerprint
	cacheExpired, err := requestCacheExpired(ctx, view, request.Fingerprint(), cacheOptions.MaxAge)
	if err != nil {
		return nil, fmt.Errorf("failed to check cache expiration: %w", err)
	}

	if ((view.HasTable() && tableExists) || !view.HasTable()) && !cacheExpired {
		return readCachedViewData(ctx, view, request)
	}

	return handleViewRefresh(ctx, view, cacheOptions, tableExists, request)
}

// handleViewRefresh deduplicates concurrent view refresh operations using singleflight
func handleViewRefresh(ctx context.Context, view *v1.View, cacheOptions *v1.CacheOptions, tableExists bool, request *requestOpt) (*api.ViewResult, error) {
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

		// Use a key that includes both view ID and request fingerprint for deduplication
		refreshKey := fmt.Sprintf("%s:%s", view.GetUID(), request.Fingerprint())
		res, refreshErr, _ := refreshGroup.Do(refreshKey, func() (any, error) {
			return populateView(refreshCtx, view, request)
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
				return readCachedViewData(ctx, view, request)
			}

			return nil, fmt.Errorf("failed to refresh view %s: %w", view.GetNamespacedName(), err)
		}

		return result, nil

	case <-time.After(cacheOptions.RefreshTimeout):
		if tableExists {
			ctx.Logger.Debugf("view %s refresh timeout reached. returning cached data", view.GetNamespacedName())
			return readCachedViewData(ctx, view, request)
		}

		return nil, fmt.Errorf("view %s refresh timeout reached. try again", view.GetNamespacedName())
	}
}

// readCachedViewData reads cached data from the view table
func readCachedViewData(ctx context.Context, view *v1.View, request *requestOpt) (*api.ViewResult, error) {
	columns := view.Spec.Columns
	if view.HasTable() {
		columns = append(view.Spec.Columns, pkgView.ColumnDef{
			Name: pkgView.ReservedColumnAttributes,
			Type: pkgView.ColumnTypeAttributes,
		})
	}

	tableName := view.TableName()
	var rows []pkgView.Row
	if request.includeRows {
		var err error
		rows, err = pkgView.ReadViewTable(ctx, columns, tableName, request.Fingerprint())
		if err != nil {
			return nil, fmt.Errorf("failed to read view table: %w", err)
		}
	}

	var panelResult models.ViewPanel
	if err := ctx.DB().Where("view_id = ? AND request_fingerprint = ?", view.GetUID(), request.Fingerprint()).Find(&panelResult).Error; err != nil {
		return nil, fmt.Errorf("failed to find panel results: %w", err)
	}

	var finalPanelResults []api.PanelResult
	if len(panelResult.Results) > 0 {
		if err := json.Unmarshal(panelResult.Results, &finalPanelResults); err != nil {
			return nil, fmt.Errorf("failed to unmarshal panel results: %w", err)
		}
	}

	result := &api.ViewResult{
		Namespace: view.Namespace,
		Name:      view.Name,
		Title:     view.Spec.Display.Title,
		Icon:      view.Spec.Display.Icon,

		Columns: columns,
		Rows:    rows,
		Panels:  finalPanelResults,
	}

	for _, filter := range view.Spec.Templating {
		if len(filter.Values) > 0 {
			result.Filters = append(result.Filters, api.ViewVariableWithOptions{
				ViewVariable: filter,
				Options:      filter.Values,
			})
		} else if filter.ValueFrom != nil {
			if !filter.ValueFrom.Config.IsEmpty() {
				resources, err := query.FindConfigsByResourceSelector(ctx, valueFromMaxResults, filter.ValueFrom.Config)
				if err != nil {
					return nil, fmt.Errorf("failed to get resources for filter %s: %w", filter.Key, err)
				}

				values := lo.Map(resources, func(r models.ConfigItem, _ int) string {
					return lo.FromPtr(r.Name)
				})
				result.Filters = append(result.Filters, api.ViewVariableWithOptions{
					ViewVariable: filter,
					Options:      values,
				})
			}
		}
	}

	// Get the last refresh time for this specific request fingerprint
	lastRan, err := getRequestLastRan(ctx, string(view.GetUID()), request.Fingerprint())
	if err != nil {
		ctx.Logger.Warnf("failed to get request last ran: %v", err)
	} else if lastRan != nil {
		result.LastRefreshedAt = *lastRan
	}

	columnOptions, err := getColumnOptions(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to get column options: %w", err)
	}
	result.ColumnOptions = columnOptions

	return result, nil
}

// populateView runs the view queries and saves to the view table.
func populateView(ctx context.Context, view *v1.View, request *requestOpt) (*api.ViewResult, error) {
	result, err := Run(ctx, view, request)
	if err != nil {
		return nil, fmt.Errorf("failed to run view: %w", err)
	}

	refreshedAt := time.Now().Truncate(time.Second)
	err = ctx.Transaction(func(ctx context.Context, span trace.Span) error {
		if view.HasTable() {
			if err := pkgView.CreateViewTable(ctx, view.TableName(), view.Spec.Columns); err != nil {
				return fmt.Errorf("failed to create view table: %w", err)
			}
		}

		if err := persistViewData(ctx, view, result, request); err != nil {
			return err
		}

		if err := updateRequestLastRan(ctx, string(view.GetUID()), request.Fingerprint(), refreshedAt); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return result, err
	}

	if !request.includeRows {
		result.Rows = nil // don't return rows. UI uses postgREST to get the table rows.
	}
	result.LastRefreshedAt = refreshedAt

	columnOptions, err := getColumnOptions(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to get column options: %w", err)
	}
	result.ColumnOptions = columnOptions

	return result, nil
}

// persistViewData saves view rows and panel results to their respective tables
func persistViewData(ctx context.Context, view *v1.View, result *api.ViewResult, request *requestOpt) error {
	tableName := view.TableName()

	// Save view rows to the dedicated table
	if view.HasTable() {
		if err := pkgView.InsertViewRows(ctx, tableName, result.Columns, result.Rows, request.Fingerprint()); err != nil {
			return fmt.Errorf("failed to insert view rows: %w", err)
		}
	}

	// Save panel results if any exist
	if len(result.Panels) > 0 {
		uid, err := view.GetUUID()
		if err != nil {
			return fmt.Errorf("failed to get view uid: %w", err)
		}

		if err := db.InsertPanelResults(ctx, uid, result.Panels, request.Fingerprint()); err != nil {
			return fmt.Errorf("failed to insert view panels: %w", err)
		}
	}

	return nil
}

// getRequestLastRan retrieves the last run time for a specific request fingerprint
func getRequestLastRan(ctx context.Context, viewID string, fingerprint string) (*time.Time, error) {
	var requestLastRan types.JSONStringMap
	if err := ctx.DB().Model(&models.View{}).Select("request_last_ran").Where("id = ?", viewID).Scan(&requestLastRan).Error; err != nil {
		return nil, fmt.Errorf("failed to get view: %w", err)
	}

	timeStr, exists := requestLastRan[fingerprint]
	if !exists {
		return nil, nil
	}

	parsedTime, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cached time: %w", err)
	}

	return &parsedTime, nil
}

// updateRequestLastRan updates the last run time for a specific request fingerprint
func updateRequestLastRan(ctx context.Context, viewID string, fingerprint string, refreshedAt time.Time) error {
	// Get current request_last_ran
	var view models.View
	if err := ctx.DB().Select("request_last_ran").Where("id = ?", viewID).Find(&view).Error; err != nil {
		return fmt.Errorf("failed to get view: %w", err)
	}

	// Initialize or update the cache map
	var requestLastRan map[string]string
	if view.RequestLastRan != nil {
		if err := json.Unmarshal(view.RequestLastRan, &requestLastRan); err != nil {
			return fmt.Errorf("failed to unmarshal request last ran: %w", err)
		}
	} else {
		requestLastRan = make(map[string]string)
	}

	requestLastRan[fingerprint] = refreshedAt.Format(time.RFC3339)

	// Marshal back to JSON
	updatedCache, err := json.Marshal(requestLastRan)
	if err != nil {
		return fmt.Errorf("failed to marshal request last ran: %w", err)
	}

	// Update database
	return ctx.DB().Model(&models.View{}).
		Where("id = ?", viewID).
		Update("request_last_ran", updatedCache).Error
}

// requestCacheExpired checks if cache has expired for a specific request fingerprint
func requestCacheExpired(ctx context.Context, view *v1.View, fingerprint string, maxAge time.Duration) (bool, error) {
	lastRan, err := getRequestLastRan(ctx, string(view.GetUID()), fingerprint)
	if err != nil {
		return true, fmt.Errorf("failed to get request last ran: %w", err)
	}

	if lastRan == nil {
		return true, nil // Never run before
	}

	return time.Since(*lastRan) > maxAge, nil
}

// getColumnOptions retrieves distinct values for columns with multiselect filters
// The UI uses this to populate the filters.
func getColumnOptions(ctx context.Context, view *v1.View) (map[string][]string, error) {
	if !view.HasTable() {
		return nil, nil
	}

	columnOptions := make(map[string][]string)
	tableName := view.TableName()

	// Find columns with multiselect filters
	for _, column := range view.Spec.Columns {
		if column.Filter != nil && column.Filter.Type == pkgView.ColumnFilterTypeMultiSelect {
			var values []string
			columnName := pq.QuoteIdentifier(column.Name)

			if err := ctx.DB().Table(tableName).
				Distinct(columnName).
				Where(columnName+" IS NOT NULL").
				Pluck(columnName, &values).Error; err != nil {
				return nil, fmt.Errorf("failed to get distinct values for column %s: %w", columnName, err)
			}

			columnOptions[column.Name] = values
		}
	}

	return columnOptions, nil
}
