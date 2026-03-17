package views

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
	gocache "github.com/patrickmn/go-cache"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

const (
	defaultScopeCacheTTL = 2 * time.Minute
	scopeConfigsCacheKey = "scope-configs-with-targets"
)

var scopeCache = gocache.New(defaultScopeCacheTTL, defaultScopeCacheTTL*2)

func FlushScopeCache() {
	scopeCache.Flush()
}

type scopeConfig struct {
	scopeID   string
	selectors []types.ResourceSelector
}

// getScopeConfigs returns cached scope configs with config/global targets.
func getScopeConfigs(ctx context.Context) ([]scopeConfig, error) {
	if cached, found := scopeCache.Get(scopeConfigsCacheKey); found {
		return cached.([]scopeConfig), nil
	}

	var scopes []models.Scope
	if err := ctx.DB().
		Where("deleted_at IS NULL").
		Find(&scopes).Error; err != nil {
		return nil, fmt.Errorf("failed to load scopes: %w", err)
	}

	var scopeConfigs []scopeConfig
	for _, scope := range scopes {
		if len(scope.Targets) == 0 {
			continue
		}

		var targets []v1.ScopeTarget
		if err := json.Unmarshal(scope.Targets, &targets); err != nil {
			return nil, fmt.Errorf("failed to unmarshal targets for scope %s: %w", scope.ID, err)
		}

		var selectors []types.ResourceSelector
		for _, target := range targets {
			var selector *types.ResourceSelector
			switch {
			case target.Config != nil:
				selector = &types.ResourceSelector{
					Agent:       target.Config.Agent,
					Name:        target.Config.Name,
					Namespace:   target.Config.Namespace,
					TagSelector: target.Config.TagSelector,
				}
			case target.Global != nil:
				selector = &types.ResourceSelector{
					Agent:       target.Global.Agent,
					Name:        target.Global.Name,
					Namespace:   target.Global.Namespace,
					TagSelector: target.Global.TagSelector,
				}
			}

			if selector != nil {
				selectors = append(selectors, *selector)
			}
		}

		if len(selectors) > 0 {
			scopeConfigs = append(scopeConfigs, scopeConfig{
				scopeID:   scope.ID.String(),
				selectors: selectors,
			})
		}
	}

	scopeCache.SetDefault(scopeConfigsCacheKey, scopeConfigs)
	return scopeConfigs, nil
}

// computeGrantsForConfigResults computes scope grants for each row in config query results
// Grants are determined by matching scope selectors against the config in each row
func computeGrantsForConfigResults(ctx context.Context, results []dataquery.QueryResultRow) ([]dataquery.QueryResultRow, error) {
	if len(results) == 0 {
		return results, nil
	}

	scopeConfigs, err := getScopeConfigs(ctx)
	if err != nil {
		return nil, err
	}

	// Process results to compute grants
	for i := range results {
		row := results[i]
		grantsSet := make(map[string]struct{})

		// Cast row to ResourceSelectableMap for matching
		rowMap := types.ResourceSelectableMap(row)

		// Match each scope against this row
		for _, sc := range scopeConfigs {
			for _, selector := range sc.selectors {
				if selector.Matches(rowMap) {
					grantsSet[sc.scopeID] = struct{}{}
					break
				}
			}
		}

		// Set grants field in result row
		// NULL if no scopes match
		var grantsValue any
		if len(grantsSet) > 0 {
			grants := make([]string, 0, len(grantsSet))
			for scopeID := range grantsSet {
				grants = append(grants, scopeID)
			}
			grantsValue = grants
		}
		row[pkgView.ReservedColumnGrants] = grantsValue
		results[i] = row
	}

	return results, nil
}
