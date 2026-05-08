package rbac_report

import (
	"sort"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

type Options struct {
	Title             string
	Selectors         []types.ResourceSelector
	Recursive         bool
	StaleDays         int
	ReviewOverdueDays int
	ChangelogSince    time.Duration
	View              string
}

func (o Options) WithDefaults() Options {
	if o.ChangelogSince == 0 {
		o.ChangelogSince = 90 * 24 * time.Hour
	}
	if o.Title == "" {
		o.Title = "RBAC Report"
	}
	return o
}

func BuildReport(ctx context.Context, opts Options) (*api.RBACReport, error) {
	opts = opts.WithDefaults()

	rows, err := db.GetRBACAccess(ctx, opts.Selectors, opts.Recursive)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query RBAC access")
	}

	configIDs := extractUniqueConfigIDs(rows)
	ctx.Logger.V(3).Infof("Found %d access rows across %d config items", len(rows), len(configIDs))

	configItems, err := query.GetConfigsByIDs(ctx, configIDs)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to load config items")
	}
	configMap := buildConfigMap(configItems)

	resources := groupByConfigItem(rows, opts, configMap)
	summary := computeSummary(resources)

	since := time.Now().Add(-opts.ChangelogSince)
	changelog, err := db.GetRBACChangelog(ctx, configIDs, since)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query RBAC changelog")
	}
	if changelog == nil {
		changelog = []api.RBACChangeEntry{}
	}

	attachChangelogToResources(resources, changelog)

	tempAccess, err := db.GetRBACTemporaryAccess(ctx, configIDs, since)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query temporary access")
	}
	attachTemporaryAccessToResources(resources, tempAccess)

	ctx.Logger.V(3).Infof("Changelog: %d entries, temporary access: %d entries", len(changelog), len(tempAccess))

	subject, parents := resolveSubjectAndParents(ctx, configItems, configMap)

	report := &api.RBACReport{
		Title:       opts.Title,
		Query:       formatSelectors(opts.Selectors),
		GeneratedAt: time.Now(),
		Subject:     subject,
		Parents:     parents,
		Resources:   resources,
		Changelog:   changelog,
		Summary:     summary,
	}

	if opts.View == "user" {
		report.Users = groupByUser(rows, opts, configMap)
	}

	return report, nil
}

func buildConfigMap(items []models.ConfigItem) map[string]models.ConfigItem {
	m := make(map[string]models.ConfigItem, len(items))
	for _, ci := range items {
		m[ci.ID.String()] = ci
	}
	return m
}

func attachChangelogToResources(resources []api.RBACResource, changelog []api.RBACChangeEntry) {
	byConfig := make(map[string][]api.RBACChangeEntry)
	for _, entry := range changelog {
		if entry.ConfigID != "" {
			byConfig[entry.ConfigID] = append(byConfig[entry.ConfigID], entry)
		}
	}
	for i := range resources {
		if entries, ok := byConfig[resources[i].ConfigID]; ok {
			resources[i].Changelog = entries
		}
	}
}

func attachTemporaryAccessToResources(resources []api.RBACResource, entries []api.RBACTemporaryAccess) {
	byConfig := make(map[string][]api.RBACTemporaryAccess)
	for _, entry := range entries {
		byConfig[entry.ConfigID] = append(byConfig[entry.ConfigID], entry)
	}
	for i := range resources {
		if entries, ok := byConfig[resources[i].ConfigID]; ok {
			resources[i].TemporaryAccess = entries
		}
	}
}

func extractUniqueConfigIDs(rows []db.RBACAccessRow) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{})
	var ids []uuid.UUID
	for _, r := range rows {
		if _, ok := seen[r.ConfigID]; !ok {
			seen[r.ConfigID] = struct{}{}
			ids = append(ids, r.ConfigID)
		}
	}
	return ids
}

func groupByConfigItem(rows []db.RBACAccessRow, opts Options, configMap map[string]models.ConfigItem) []api.RBACResource {
	staleThreshold := time.Now().AddDate(0, 0, -opts.StaleDays)
	reviewThreshold := time.Now().AddDate(0, 0, -opts.ReviewOverdueDays)

	grouped := make(map[uuid.UUID]*api.RBACResource)
	var order []uuid.UUID

	for _, row := range rows {
		resource, ok := grouped[row.ConfigID]
		if !ok {
			resource = &api.RBACResource{
				ConfigID:   row.ConfigID.String(),
				ConfigName: row.ConfigName,
				ConfigType: row.ConfigType,
			}
			if ci, found := configMap[row.ConfigID.String()]; found {
				enrichResourceFromConfigItem(resource, ci)
			}
			grouped[row.ConfigID] = resource
			order = append(order, row.ConfigID)
		}

		isStale := opts.StaleDays > 0 && (row.LastSignedInAt == nil || row.LastSignedInAt.Before(staleThreshold))
		isReviewOverdue := opts.ReviewOverdueDays > 0 && (row.LastReviewedAt == nil || row.LastReviewedAt.Before(reviewThreshold))

		resource.Users = append(resource.Users, api.RBACUserRole{
			UserID:          row.UserID.String(),
			UserName:        row.UserName,
			Email:           row.Email,
			Role:            row.Role,
			RoleExternalIDs: []string(row.RoleExternalIDs),
			RoleSource:      row.RoleSource(),
			SourceSystem:    row.UserType,
			CreatedAt:       row.CreatedAt,
			LastSignedInAt:  row.LastSignedInAt,
			LastReviewedAt:  row.LastReviewedAt,
			IsStale:         isStale,
			IsReviewOverdue: isReviewOverdue,
		})
	}

	return lo.Map(order, func(id uuid.UUID, _ int) api.RBACResource {
		return *grouped[id]
	})
}

func resolveSubjectAndParents(ctx context.Context, configItems []models.ConfigItem, configMap map[string]models.ConfigItem) (*models.ConfigItem, []models.ConfigItem) {
	if len(configItems) == 0 {
		return nil, nil
	}

	first := configItems[0]

	var parents []models.ConfigItem
	current := first
	for current.ParentID != nil {
		parent, ok := configMap[current.ParentID.String()]
		if !ok {
			loaded, err := query.GetConfigsByIDs(ctx, []uuid.UUID{*current.ParentID})
			if err != nil || len(loaded) == 0 {
				break
			}
			parent = loaded[0]
			configMap[parent.ID.String()] = parent
		}
		parents = append(parents, parent)
		current = parent
	}

	// Reverse so root is first
	for i, j := 0, len(parents)-1; i < j; i, j = i+1, j-1 {
		parents[i], parents[j] = parents[j], parents[i]
	}

	return &first, parents
}

func enrichResourceFromConfigItem(resource *api.RBACResource, ci models.ConfigItem) {
	resource.ConfigClass = ci.ConfigClass
	resource.Path = ci.Path
	if ci.ParentID != nil {
		resource.ParentID = ci.ParentID.String()
	}
	if ci.Status != nil {
		resource.Status = *ci.Status
	}
	if ci.Health != nil {
		resource.Health = string(*ci.Health)
	}
	if ci.Description != nil {
		resource.Description = *ci.Description
	}
	resource.Tags = ci.Tags
	if ci.Labels != nil {
		resource.Labels = *ci.Labels
	}
	resource.CreatedAt = &ci.CreatedAt
	resource.UpdatedAt = ci.UpdatedAt
}

func groupByUser(rows []db.RBACAccessRow, opts Options, configMap map[string]models.ConfigItem) []api.RBACUserReport {
	staleThreshold := time.Now().AddDate(0, 0, -opts.StaleDays)
	reviewThreshold := time.Now().AddDate(0, 0, -opts.ReviewOverdueDays)

	type userEntry struct {
		report api.RBACUserReport
		order  int
	}
	grouped := make(map[string]*userEntry)
	seq := 0

	for _, row := range rows {
		key := strings.ToLower(row.Email)
		if key == "" {
			key = strings.ToLower(row.UserName)
		}
		entry, ok := grouped[key]
		if !ok {
			entry = &userEntry{
				report: api.RBACUserReport{
					UserID:       row.UserID.String(),
					UserName:     row.UserName,
					Email:        row.Email,
					SourceSystem: row.UserType,
				},
				order: seq,
			}
			seq++
			grouped[key] = entry
		}

		if row.LastSignedInAt != nil {
			if entry.report.LastSignedInAt == nil || row.LastSignedInAt.After(*entry.report.LastSignedInAt) {
				entry.report.LastSignedInAt = row.LastSignedInAt
			}
		}

		isStale := opts.StaleDays > 0 && (row.LastSignedInAt == nil || row.LastSignedInAt.Before(staleThreshold))
		isReviewOverdue := opts.ReviewOverdueDays > 0 && (row.LastReviewedAt == nil || row.LastReviewedAt.Before(reviewThreshold))

		res := api.RBACUserResource{
			ConfigID:        row.ConfigID.String(),
			ConfigName:      row.ConfigName,
			ConfigType:      row.ConfigType,
			Role:            row.Role,
			RoleExternalIDs: []string(row.RoleExternalIDs),
			RoleSource:      row.RoleSource(),
			CreatedAt:       row.CreatedAt,
			LastSignedInAt:  row.LastSignedInAt,
			LastReviewedAt:  row.LastReviewedAt,
			IsStale:         isStale,
			IsReviewOverdue: isReviewOverdue,
		}
		if ci, found := configMap[row.ConfigID.String()]; found {
			res.ConfigClass = ci.ConfigClass
			res.Path = ci.Path
			if ci.Status != nil {
				res.Status = *ci.Status
			}
			if ci.Health != nil {
				res.Health = string(*ci.Health)
			}
			res.Tags = ci.Tags
			if ci.Labels != nil {
				res.Labels = *ci.Labels
			}
		}
		entry.report.Resources = append(entry.report.Resources, res)
	}

	users := make([]api.RBACUserReport, 0, len(grouped))
	for _, e := range grouped {
		sort.Slice(e.report.Resources, func(i, j int) bool {
			if e.report.Resources[i].ConfigType != e.report.Resources[j].ConfigType {
				return e.report.Resources[i].ConfigType < e.report.Resources[j].ConfigType
			}
			return e.report.Resources[i].ConfigName < e.report.Resources[j].ConfigName
		})
		users = append(users, e.report)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].UserName) < strings.ToLower(users[j].UserName)
	})
	return users
}

func computeSummary(resources []api.RBACResource) api.RBACSummary {
	uniqueUsers := make(map[string]struct{})
	var summary api.RBACSummary

	summary.TotalResources = len(resources)
	for _, r := range resources {
		for _, u := range r.Users {
			uniqueUsers[u.UserID] = struct{}{}
			if u.IsStale {
				summary.StaleAccessCount++
			}
			if u.IsReviewOverdue {
				summary.OverdueReviews++
			}
			if u.RoleSource == "direct" {
				summary.DirectAssignments++
			} else {
				summary.GroupAssignments++
			}
		}
	}
	summary.TotalUsers = len(uniqueUsers)

	return summary
}

func formatSelectors(selectors []types.ResourceSelector) string {
	parts := make([]string, 0, len(selectors))
	for _, s := range selectors {
		if str := s.String(); str != "" {
			parts = append(parts, str)
		}
	}
	return strings.Join(parts, "; ")
}
