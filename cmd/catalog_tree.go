package cmd

import (
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

var (
	treeDirection string
	treeSoft      bool
	treeHealth    bool
)

var Tree = &cobra.Command{
	Use:   "tree <ID|QUERY>",
	Short: "Show config items' parent/child hierarchy and relationships as a tree",
	Long: `Display config item hierarchy (parents + children) and relationships as a tree.
When the query matches multiple configs, each tree is rendered in sequence.

Parent/child edges are shown normally. Relationship edges are marked with ~.

Examples:
  catalog tree 018f4e6a-1234-5678-9abc-def012345678
  catalog tree type=Kubernetes::Namespace name=default
  catalog tree <id> --direction=incoming
  catalog tree <id> --direction=outgoing --soft`,
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			return err
		}
		defer stop()

		result, err := runCatalogTree(ctx, args)
		if err != nil {
			return err
		}

		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		return nil
	},
}

type CatalogTreeResult struct {
	*query.ConfigTreeNode
}

func (r CatalogTreeResult) Pretty() api.Text {
	return treeNodeLabel(r.ConfigTreeNode)
}

func (r CatalogTreeResult) GetChildren() []api.TreeNode {
	nodes := make([]api.TreeNode, len(r.Children))
	for i, c := range r.Children {
		nodes[i] = treeNodeAdapter{c}
	}
	return nodes
}

// CatalogTreeResults holds the merged forest built from all matched configs.
// Multiple matches that share ancestors collapse into a single tree; matches
// from unrelated ancestry sit side-by-side as sibling roots.
type CatalogTreeResults struct {
	Roots        []CatalogTreeResult `json:"roots"`
	MatchedCount int                 `json:"matchedCount"`
}

func (r CatalogTreeResults) Pretty() api.Text {
	t := clicky.Text("")
	t = t.Add(clicky.Text(fmt.Sprintf("%d matching configs", r.MatchedCount), "font-bold text-gray-700"))
	for i := range r.Roots {
		t = t.NewLine().Append(api.NewTree[api.TreeNode](r.Roots[i]))
	}
	return t
}

type treeNodeAdapter struct {
	*query.ConfigTreeNode
}

func (n treeNodeAdapter) Pretty() api.Text {
	return treeNodeLabel(n.ConfigTreeNode)
}

func (n treeNodeAdapter) GetChildren() []api.TreeNode {
	nodes := make([]api.TreeNode, len(n.Children))
	for i, c := range n.Children {
		nodes[i] = treeNodeAdapter{c}
	}
	return nodes
}

func treeNodeLabel(n *query.ConfigTreeNode) api.Text {
	isTarget := n.EdgeType == "target"
	isRelated := n.EdgeType == "related"

	t := clicky.Text("")
	if isRelated {
		t = t.AddText("~ ", "text-purple-500")
	}
	if n.Type != nil {
		t = t.Add(clicky.Text(lo.FromPtr(n.Type), "text-xs text-gray-600"))
		t = t.AddText("/")
	}
	style := "font-bold"
	if isTarget {
		style = "font-bold underline"
	}
	t = t.AddText(lo.FromPtrOr(n.Name, "<unnamed>"), style)
	if isRelated && n.Relation != "" {
		t = t.AddText(" ").Add(clicky.Text(n.Relation, "text-xs text-purple-400 italic"))
	}
	if treeHealth {
		if n.Health != nil {
			t = t.AddText(" ").Add(n.Health.Pretty())
		}
		if n.Status != nil && *n.Status != "" {
			t = t.AddText(" ").Add(clicky.Text(*n.Status, "text-xs text-gray-500"))
		}
	}
	if clicky.Flags.LevelCount >= 2 {
		t = t.AddText(" ").Add(clicky.Text(n.ID.String(), "text-xs font-mono text-gray-400"))
		if n.Path != "" {
			t = t.AddText(" ").Add(clicky.Text(n.Path, "text-xs text-gray-400"))
		}
	}
	return t
}

func runCatalogTree(ctx context.Context, args []string) (*CatalogTreeResults, error) {
	configs, err := resolveConfigsForCommand(ctx, args)
	if err != nil {
		return nil, err
	}

	switch treeDirection {
	case "all", "incoming", "outgoing":
	default:
		return nil, fmt.Errorf("invalid --direction %q: must be all, incoming, or outgoing", treeDirection)
	}

	relType := query.Hard
	if treeSoft {
		relType = query.Both
	}

	opts := query.ConfigTreeOptions{
		Direction: query.RelationDirection(treeDirection),
		Incoming:  relType,
		Outgoing:  relType,
	}

	trees := make([]*query.ConfigTreeNode, 0, len(configs))
	for i := range configs {
		tree, err := query.ConfigTree(ctx, configs[i].ID, opts)
		if err != nil {
			return nil, err
		}
		if tree != nil {
			trees = append(trees, tree)
		}
	}

	merged := query.MergeConfigTrees(trees)
	roots := make([]CatalogTreeResult, len(merged))
	for i, t := range merged {
		roots[i] = CatalogTreeResult{ConfigTreeNode: t}
	}
	return &CatalogTreeResults{Roots: roots, MatchedCount: len(configs)}, nil
}

func init() {
	Tree.Flags().StringVar(&treeDirection, "direction", "all", "Relationship direction: all, incoming, outgoing")
	Tree.Flags().BoolVar(&treeSoft, "soft", false, "Include soft relationships")
	Tree.Flags().BoolVar(&treeHealth, "health", false, "Show health and status")
	clicky.BindAllFlags(Tree.PersistentFlags(), "format")
	clicky.RegisterSubCommand("catalog", Tree)
}
