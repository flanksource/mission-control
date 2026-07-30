package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

// Selection keeps explicitly selected roots separate from the complete set of
// report items. ItemsByRoot assigns every included config to one root so nested
// or overlapping roots do not duplicate changes, insights, or access data.
type Selection struct {
	Roots       []models.ConfigItem
	Items       []models.ConfigItem
	RootIDs     map[uuid.UUID]bool
	ItemsByRoot map[uuid.UUID][]models.ConfigItem
}

// ResolveSelection expands every root when recursive is enabled. Explicit
// roots always remain selected; filters apply only to discovered descendants.
func ResolveSelection(ctx context.Context, roots []models.ConfigItem, recursive bool, filter string) (Selection, error) {
	recursiveRoots := make(map[uuid.UUID]bool, len(roots))
	if recursive {
		for _, root := range roots {
			recursiveRoots[root.ID] = true
		}
	}
	return ResolveSelectionRoots(ctx, roots, recursiveRoots, filter)
}

// ResolveSelectionRoots expands only roots present in recursiveRoots. This
// preserves the HTTP API's per-root includeChildren behavior.
func ResolveSelectionRoots(ctx context.Context, roots []models.ConfigItem, recursiveRoots map[uuid.UUID]bool, filter string) (Selection, error) {
	selection := Selection{
		RootIDs:     make(map[uuid.UUID]bool),
		ItemsByRoot: make(map[uuid.UUID][]models.ConfigItem),
	}
	rootOrder := make(map[uuid.UUID]int)
	for _, root := range roots {
		if selection.RootIDs[root.ID] {
			continue
		}
		rootOrder[root.ID] = len(selection.Roots)
		selection.RootIDs[root.ID] = true
		selection.Roots = append(selection.Roots, root)
	}
	if len(selection.Roots) == 0 {
		return selection, fmt.Errorf("no root config items provided")
	}

	candidates := make(map[uuid.UUID]models.ConfigItem, len(selection.Roots))
	candidateRoots := make(map[uuid.UUID][]uuid.UUID)
	for _, root := range selection.Roots {
		candidates[root.ID] = root
		candidateRoots[root.ID] = append(candidateRoots[root.ID], root.ID)
	}

	for _, root := range selection.Roots {
		if !recursiveRoots[root.ID] {
			continue
		}
		tree, err := query.ConfigTree(ctx, root.ID, query.ConfigTreeOptions{})
		if err != nil {
			return selection, fmt.Errorf("expand descendants for %s: %w", root.ID, err)
		}
		if tree == nil {
			continue
		}
		ids := tree.OutgoingIDs()
		items, err := query.GetConfigsByIDs(ctx, ids)
		if err != nil {
			return selection, fmt.Errorf("load descendants for %s: %w", root.ID, err)
		}
		for _, item := range items {
			candidates[item.ID] = item
			candidateRoots[item.ID] = appendUniqueID(candidateRoots[item.ID], root.ID)
		}
	}

	allowed := make(map[uuid.UUID]bool, len(candidates))
	for id := range selection.RootIDs {
		allowed[id] = true
	}
	if filter == "" {
		for id := range candidates {
			allowed[id] = true
		}
	} else {
		var descendantIDs []string
		for id := range candidates {
			if !selection.RootIDs[id] {
				descendantIDs = append(descendantIDs, id.String())
			}
		}
		if len(descendantIDs) > 0 {
			matched, err := query.FindConfigsByResourceSelector(ctx, -1, types.ResourceSelector{
				ID:     strings.Join(descendantIDs, ","),
				Search: filter,
			})
			if err != nil {
				return selection, fmt.Errorf("filter descendants: %w", err)
			}
			for _, item := range matched {
				allowed[item.ID] = true
			}
		}
	}

	items := make([]models.ConfigItem, 0, len(allowed))
	for id, item := range candidates {
		if allowed[id] {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftRoot, leftIsRoot := rootOrder[items[i].ID]
		rightRoot, rightIsRoot := rootOrder[items[j].ID]
		if leftIsRoot != rightIsRoot {
			return leftIsRoot
		}
		if leftIsRoot {
			return leftRoot < rightRoot
		}
		left := fmt.Sprintf("%06d/%s/%s/%s", pathDepth(items[i]), items[i].GetType(), items[i].GetName(), items[i].ID)
		right := fmt.Sprintf("%06d/%s/%s/%s", pathDepth(items[j]), items[j].GetType(), items[j].GetName(), items[j].ID)
		return left < right
	})

	for _, item := range items {
		owner := owningRoot(item, selection.RootIDs)
		if owner == uuid.Nil {
			for _, candidate := range candidateRoots[item.ID] {
				if selection.RootIDs[candidate] {
					owner = candidate
					break
				}
			}
		}
		if owner == uuid.Nil {
			continue
		}
		selection.Items = append(selection.Items, item)
		selection.ItemsByRoot[owner] = append(selection.ItemsByRoot[owner], item)
	}

	return selection, nil
}

func owningRoot(item models.ConfigItem, roots map[uuid.UUID]bool) uuid.UUID {
	if roots[item.ID] {
		return item.ID
	}
	parents := parentIDsFromPath(&item)
	for i := len(parents) - 1; i >= 0; i-- {
		if roots[parents[i]] {
			return parents[i]
		}
	}
	return uuid.Nil
}

func pathDepth(item models.ConfigItem) int {
	if item.Path == "" {
		return 0
	}
	return len(strings.Split(item.Path, "."))
}

func appendUniqueID(ids []uuid.UUID, id uuid.UUID) []uuid.UUID {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}
