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

func WithVariableDefault(key, value string) ViewOption {
	return func(c *requestOpt) {
		if c.variables == nil {
			c.variables = make(map[string]string)
		}

		if _, ok := c.variables[key]; !ok {
			c.variables[key] = value
		}
	}
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

// populateViewVariables populates view variables with their options, handling dependencies
// and user selections. Variables are processed in dependency order, with dependent
// variables being templated using the values of their dependencies.
// Returns both the populated variables with options and the templated variable definitions.
func populateViewVariables(ctx context.Context, variables []api.ViewVariable, userVariables map[string]string) ([]api.ViewVariableWithOptions, []api.ViewVariable, error) {
	if userVariables == nil {
		userVariables = make(map[string]string)
	}

	levels, err := organizeVariablesByLevels(variables)
	if err != nil {
		return nil, nil, err
	}
	variableValues := make(map[string]string)
	var result []api.ViewVariableWithOptions
	templatedVariables := make([]api.ViewVariable, len(variables))

	// Create a map to track variable indices for updating templated variables
	variableIndexMap := make(map[string]int)
	for i, v := range variables {
		variableIndexMap[v.Key] = i
		templatedVariables[i] = v // Initialize with original values
	}

	for _, level := range levels {
		for _, variable := range level {
			populatedVar, err := processVariable(ctx, variable, variableValues, userVariables)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to process variable %s: %w", variable.Key, err)
			}

			result = append(result, populatedVar)

			// Track selected value for templating dependent variables
			if populatedVar.Default != "" {
				variableValues[variable.Key] = populatedVar.Default
			}

			// Update templated variable if it has dependencies
			if len(variable.DependsOn) > 0 && variable.ValueFrom != nil && !variable.ValueFrom.Config.IsEmpty() {
				templatedSelector, err := templateResourceSelector(ctx, variable.ValueFrom.Config, variableValues)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to template config selector for variable %s: %w", variable.Key, err)
				} else {
					idx := variableIndexMap[variable.Key]
					templatedVariables[idx].ValueFrom.Config = templatedSelector
				}
			}
		}
	}

	return result, templatedVariables, nil
}

// processVariable handles the complete processing of a single variable including
// population and value selection
func processVariable(ctx context.Context, variable api.ViewVariable, variableValues, userVariables map[string]string) (api.ViewVariableWithOptions, error) {
	// Populate the variable with its options
	variableWithOptions, err := populateVariable(ctx, variable, variableValues)
	if err != nil {
		return api.ViewVariableWithOptions{}, err
	}

	// Determine the selected value (user selection takes precedence)
	selectedValue := selectVariableValue(variable.Key, variableWithOptions, userVariables)

	// Set the selected value as the default in the response
	if selectedValue != "" {
		variableWithOptions.Default = selectedValue
	}

	return variableWithOptions, nil
}

// selectVariableValue determines the value to use for a variable, prioritizing
// user selection, then default, then first option
func selectVariableValue(key string, variable api.ViewVariableWithOptions, userVariables map[string]string) string {
	if userValue, exists := userVariables[key]; exists && userValue != "" {
		return userValue
	}

	return getDefaultValue(variable)
}

// organizeVariablesByLevels organizes variables into dependency levels using topological sort.
// Variables with no dependencies are at level 0, variables that depend only on level 0
// variables are at level 1, and so on.
func organizeVariablesByLevels(variables []api.ViewVariable) ([][]api.ViewVariable, error) {
	varMap := buildVariableMap(variables)
	depths, err := calculateVariableDepths(varMap, variables)
	if err != nil {
		return nil, err
	}
	return groupVariablesByLevel(variables, depths), nil
}

// buildVariableMap creates a map for quick variable lookup by key
func buildVariableMap(variables []api.ViewVariable) map[string]api.ViewVariable {
	varMap := make(map[string]api.ViewVariable)
	for _, v := range variables {
		varMap[v.Key] = v
	}
	return varMap
}

// calculateVariableDepths calculates the dependency depth for each variable
func calculateVariableDepths(varMap map[string]api.ViewVariable, variables []api.ViewVariable) (map[string]int, error) {
	depths := make(map[string]int)
	calculating := make(map[string]bool) // Track variables currently being calculated to detect cycles

	var calculateDepth func(string) (int, error)
	calculateDepth = func(varKey string) (int, error) {
		if depth, exists := depths[varKey]; exists {
			return depth, nil
		}

		if calculating[varKey] {
			return 0, fmt.Errorf("circular dependency detected involving variable: %s", varKey)
		}

		variable, exists := varMap[varKey]
		if !exists {
			return 0, fmt.Errorf("undefined variable referenced: %s", varKey)
		}

		calculating[varKey] = true
		defer func() { calculating[varKey] = false }()

		maxDepth := 0
		for _, dep := range variable.DependsOn {
			depDepth, err := calculateDepth(dep)
			if err != nil {
				return 0, err
			}
			if depDepth >= maxDepth {
				maxDepth = depDepth + 1
			}
		}

		depths[varKey] = maxDepth
		return maxDepth, nil
	}

	for _, variable := range variables {
		if _, err := calculateDepth(variable.Key); err != nil {
			return nil, err
		}
	}

	return depths, nil
}

// groupVariablesByLevel groups variables into levels based on their dependency depth
func groupVariablesByLevel(variables []api.ViewVariable, depths map[string]int) [][]api.ViewVariable {
	levelMap := make(map[int][]api.ViewVariable)
	maxLevel := 0

	for _, variable := range variables {
		level := depths[variable.Key]
		levelMap[level] = append(levelMap[level], variable)
		if level > maxLevel {
			maxLevel = level
		}
	}

	levels := make([][]api.ViewVariable, maxLevel+1)
	for i := 0; i <= maxLevel; i++ {
		levels[i] = levelMap[i]
	}

	return levels
}

// populateVariable populates a single variable with its options, applying templating if needed
func populateVariable(ctx context.Context, variable api.ViewVariable, variableValues map[string]string) (api.ViewVariableWithOptions, error) {
	if len(variable.Values) > 0 {
		return api.ViewVariableWithOptions{
			ViewVariable: variable,
			Options:      variable.Values,
		}, nil
	}

	if variable.ValueFrom == nil {
		return api.ViewVariableWithOptions{
			ViewVariable: variable,
			Options:      []string{},
		}, nil
	}

	if !variable.ValueFrom.Config.IsEmpty() {
		selector := variable.ValueFrom.Config

		// Template the config selector if this variable has dependencies
		if len(variable.DependsOn) > 0 {
			templatedSelector, err := templateResourceSelector(ctx, selector, variableValues)
			if err != nil {
				return api.ViewVariableWithOptions{}, fmt.Errorf("failed to template config selector: %w", err)
			}
			selector = templatedSelector
		}

		resources, err := query.FindConfigsByResourceSelector(ctx, valueFromMaxResults, selector)
		if err != nil {
			return api.ViewVariableWithOptions{}, fmt.Errorf("failed to get resources for filter %s: %w", variable.Key, err)
		}

		values := lo.Map(resources, func(r models.ConfigItem, _ int) string {
			return lo.FromPtr(r.Name)
		})

		return api.ViewVariableWithOptions{
			ViewVariable: variable,
			Options:      values,
		}, nil
	}

	return api.ViewVariableWithOptions{
		ViewVariable: variable,
		Options:      []string{},
	}, nil
}

// getDefaultValue returns the default value for a variable or the first option if no default is set
func getDefaultValue(variable api.ViewVariableWithOptions) string {
	if variable.Default != "" {
		return variable.Default
	}
	if len(variable.Options) > 0 {
		return variable.Options[0]
	}
	return ""
}

// templateResourceSelector applies templating to a resource selector using variable values
func templateResourceSelector(ctx context.Context, selector types.ResourceSelector, variableValues map[string]string) (types.ResourceSelector, error) {
	// Create template environment
	env := map[string]any{
		"var": variableValues,
	}

	// Use structemplater to template the entire selector
	st := ctx.NewStructTemplater(env, "", nil)
	if err := st.Walk(&selector); err != nil {
		return selector, fmt.Errorf("failed to template resource selector with env %v: %w", env, err)
	}

	return selector, nil
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

	// Process request options first to get user-selected variable values
	request := &requestOpt{}
	for _, opt := range opts {
		opt(request)
	}

	// Populate variables with user selections considered
	variables, templatedVariables, err := populateViewVariables(ctx, view.Spec.Templating, request.variables)
	if err != nil {
		return nil, fmt.Errorf("failed to populate view variables: %w", err)
	}

	view.Spec.Templating = templatedVariables

	cacheOptions, err := view.GetCacheOptions(lo.FromPtr(request.maxAge), lo.FromPtr(request.refreshTimeout))
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

	var result *api.ViewResult
	if ((view.HasTable() && tableExists) || !view.HasTable()) && !cacheExpired {
		result, err = readCachedViewData(ctx, view, request)
		if err != nil {
			return nil, fmt.Errorf("failed to read cached view data: %w", err)
		}
	} else {
		result, err = handleViewRefresh(ctx, view, cacheOptions, tableExists, request)
		if err != nil {
			return nil, fmt.Errorf("failed to handle view refresh: %w", err)
		}
	}

	result.Variables = variables
	return result, nil
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

		RequestFingerprint: request.Fingerprint(),
		Columns:            columns,
		Rows:               rows,
		Panels:             finalPanelResults,
	}

	for _, filter := range view.Spec.Templating {
		if len(filter.Values) > 0 {
			result.Variables = append(result.Variables, api.ViewVariableWithOptions{
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
				result.Variables = append(result.Variables, api.ViewVariableWithOptions{
					ViewVariable: filter,
					Options:      values,
				})
			}
		}
	}

	// Get the last refresh time for this specific request fingerprint
	lastRan, err := getLastRefresh(ctx, string(view.GetUID()), request.Fingerprint())
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
	result.LastRefreshedAt = lo.FromPtr(lastRan)

	return result, nil
}

// populateView runs the view queries and saves to the view table.
func populateView(ctx context.Context, view *v1.View, request *requestOpt) (*api.ViewResult, error) {
	result, err := Run(ctx, view, request)
	if err != nil {
		return nil, fmt.Errorf("failed to run view: %w", err)
	}

	err = ctx.Transaction(func(ctx context.Context, span trace.Span) error {
		if view.HasTable() {
			if err := pkgView.CreateViewTable(ctx, view.TableName(), view.Spec.Columns); err != nil {
				return fmt.Errorf("failed to create view table: %w", err)
			}
		}

		if err := persistViewData(ctx, view, result, request); err != nil {
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

	columnOptions, err := getColumnOptions(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to get column options: %w", err)
	}
	result.ColumnOptions = columnOptions
	result.LastRefreshedAt = time.Now()
	result.RequestFingerprint = request.Fingerprint()

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

	uid, err := view.GetUUID()
	if err != nil {
		return fmt.Errorf("failed to get view uid: %w", err)
	}

	if err := db.InsertPanelResults(ctx, uid, result.Panels, request.Fingerprint()); err != nil {
		return fmt.Errorf("failed to insert view panels: %w", err)
	}

	return nil
}

// getLastRefresh retrieves the last run time for a specific request fingerprint
func getLastRefresh(ctx context.Context, viewID string, fingerprint string) (*time.Time, error) {
	var panel models.ViewPanel
	if err := ctx.DB().Select("refreshed_at").Where("view_id = ? AND request_fingerprint = ?", viewID, fingerprint).Limit(1).Find(&panel).Error; err != nil {
		return nil, fmt.Errorf("failed to get view panel: %w", err)
	}

	return panel.RefreshedAt, nil
}

// requestCacheExpired checks if cache has expired for a specific request fingerprint
func requestCacheExpired(ctx context.Context, view *v1.View, fingerprint string, maxAge time.Duration) (bool, error) {
	lastRan, err := getLastRefresh(ctx, string(view.GetUID()), fingerprint)
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
