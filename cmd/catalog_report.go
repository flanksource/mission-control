package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/shutdown"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
	"github.com/flanksource/incident-commander/report/catalog"
)

var (
	catalogReportFormat   string
	catalogReportOutFile  string
	catalogReportSince    string
	catalogReportTitle    string
	catalogReportSettings string

	catalogReportChanges          bool
	catalogReportInsights         bool
	catalogReportRelationships    bool
	catalogReportAccess           bool
	catalogReportExpandGroups     bool
	catalogReportAccessLogs       bool
	catalogReportConfigJSON       bool
	catalogReportRecursive        bool
	catalogReportGroupBy          string
	catalogReportChangeArtifacts  bool
	catalogReportAudit            bool
	catalogReportLimit            int
	catalogReportMaxItems         int
	catalogReportMaxChanges       int
	catalogReportMaxItemArtifacts int

	catalogReportStaleDays         int
	catalogReportReviewOverdueDays int
	catalogReportFilters           []string
)

var CatalogReportCmd = &cobra.Command{
	Use:   "report <ID|QUERY...>",
	Short: "Generate a catalog report for one or more config items",
	Long: `Generate a PDF/HTML report for one or more config items including changes, insights,
relationships, RBAC access, and access logs.

Each positional argument is treated as a separate query. Quote tokens that
belong to the same query. Results from multiple queries are merged and
deduplicated by config ID before rendering.

Examples:
  # By config ID
  catalog report 018f4e6a-1234-5678-9abc-def012345678

  # Single query (quote tokens that belong together)
  catalog report 'type=Kubernetes::Namespace name=default'

  # Multiple queries merged into one report
  catalog report 'type=Kubernetes::Pod' 'type=Kubernetes::Namespace name=default'

  # HTML output
  catalog report 018f4e6a-... --format facet-html -o report.html

  # JSON with config body included
  catalog report 018f4e6a-... --format json --config-json`,
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
		shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)
		shutdown.WaitForSignal()

		opts, err := buildCatalogReportOptions(cmd)
		if err != nil {
			return err
		}

		queryArgs := args
		if opts.Settings != nil {
			if fq := opts.Settings.FilterQuery(); fq != "" {
				queryArgs = make([]string, len(args))
				for i, a := range args {
					queryArgs[i] = strings.TrimSpace(a + " " + fq)
				}
			}
		}

		configs, err := resolveConfigs(ctx, queryArgs, catalogReportLimit)
		if err != nil {
			return err
		}

		if catalogReportRecursive {
			configs, err = expandDescendants(ctx, configs, catalogReportLimit)
			if err != nil {
				return err
			}
		}

		result, err := catalog.Export(ctx, configs, opts, catalogReportFormat)
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
			return err
		}

		out := catalogReportOutFile
		if out == "" {
			out = "stdout"
		}

		details := fmt.Sprintf("Rendering catalog report to %s (%s) %dKB", out, catalogReportFormat, len(result.Data)/1024)
		if opts.Settings != nil {
			details += fmt.Sprintf(" settings=%s\n%s", result.Settings, opts.Settings.Pretty().ANSI())
		}
		if result.SrcDir != "" {
			details += fmt.Sprintf(" dir=%s", result.SrcDir)
		}
		if result.Entry != "" {
			details += fmt.Sprintf(" entry=%s", result.Entry)
		}
		if result.DataFile != "" {
			details += fmt.Sprintf(" data=%s", result.DataFile)
		}
		logger.Infof(details)

		if catalogReportOutFile != "" {
			if err := os.WriteFile(catalogReportOutFile, result.Data, 0600); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		} else {
			fmt.Print(string(result.Data))
		}

		return nil
	},
}

func buildCatalogReportOptions(cmd *cobra.Command) (catalog.Options, error) {
	opts := catalog.Options{
		Title:            catalogReportTitle,
		Recursive:        catalogReportRecursive,
		GroupBy:          catalogReportGroupBy,
		ChangeArtifacts:  catalogReportChangeArtifacts,
		Audit:            catalogReportAudit,
		ExpandGroups:     catalogReportExpandGroups,
		Limit:            catalogReportLimit,
		MaxItems:         catalogReportMaxItems,
		MaxChanges:       catalogReportMaxChanges,
		MaxItemArtifacts: catalogReportMaxItemArtifacts,
		Sections: api.CatalogReportSections{
			Changes:       catalogReportChanges,
			Insights:      catalogReportInsights,
			Relationships: catalogReportRelationships,
			Access:        catalogReportAccess,
			AccessLogs:    catalogReportAccessLogs,
			ConfigJSON:    catalogReportConfigJSON,
		},
	}

	if catalogReportSince != "" {
		d, err := duration.ParseDuration(catalogReportSince)
		if err != nil {
			return catalog.Options{}, fmt.Errorf("invalid --since: %w", err)
		}
		opts.Since = time.Duration(d)
	}

	settings, settingsSource, err := catalog.ResolveSettings(catalogReportSettings)
	if err != nil {
		return catalog.Options{}, fmt.Errorf("failed to load settings: %w", err)
	}
	opts.Settings = settings
	opts.SettingsPath = settingsSource

	if cmd.Flags().Changed("stale-days") {
		opts.Settings.Thresholds.StaleDays = catalogReportStaleDays
	}
	if cmd.Flags().Changed("review-overdue-days") {
		opts.Settings.Thresholds.ReviewOverdueDays = catalogReportReviewOverdueDays
	}
	if len(catalogReportFilters) > 0 {
		opts.Settings.Filters = append(opts.Settings.Filters, catalogReportFilters...)
	}

	return opts, nil
}

func init() {
	CatalogReportCmd.Flags().StringVarP(&catalogReportFormat, "format", "f", "facet-pdf", "Output format: json, facet-html, facet-pdf")
	CatalogReportCmd.Flags().StringVarP(&catalogReportOutFile, "out-file", "o", "", "Write output to file instead of stdout")
	CatalogReportCmd.Flags().StringVar(&catalogReportSince, "since", "30d", "Time range for changes and access logs (supports d/w/y e.g. 7d, 2w, 30d)")
	CatalogReportCmd.Flags().StringVar(&catalogReportTitle, "title", "", "Report title (default auto-generated)")
	CatalogReportCmd.Flags().StringVar(&report.SourceDir, "report-source", "", "Local directory or TSX file for report rendering (overrides embedded reports)")

	CatalogReportCmd.Flags().BoolVar(&catalogReportRecursive, "recursive", false, "Include all descendant config items")
	CatalogReportCmd.Flags().StringVar(&catalogReportGroupBy, "group-by", "none", "Group descendant data: 'none' (default), 'merged', or 'config'")
	CatalogReportCmd.Flags().BoolVar(&catalogReportChangeArtifacts, "change-artifacts", false, "Embed change artifacts (images/screenshots) in the report")
	CatalogReportCmd.Flags().IntVar(&catalogReportLimit, "limit", 50, "Maximum number of config items to report on, including recursive descendants (0 = unlimited)")
	CatalogReportCmd.Flags().IntVar(&catalogReportMaxItems, "max-items", 50, "Maximum items per section (changes, analyses, access, access-logs). Section-specific flags override this. (0 = unlimited)")
	CatalogReportCmd.Flags().IntVar(&catalogReportMaxChanges, "max-changes", 100, "Maximum changes per entry, overrides --max-items for the changes section (0 = unlimited)")
	CatalogReportCmd.Flags().IntVar(&catalogReportMaxItemArtifacts, "max-item-artifacts", 0, "Maximum artifacts retained per change source within a single config item (0 = unlimited). Requires --change-artifacts.")
	CatalogReportCmd.Flags().BoolVar(&catalogReportChanges, "changes", true, "Include config changes section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportInsights, "insights", true, "Include config insights section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportRelationships, "relationships", true, "Include relationships section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAccess, "access", true, "Include RBAC access section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportExpandGroups, "expand-groups", false, "Expand each group-granted access row into synthetic per-member rows (members render as indirect). Group rows are preserved.")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAccessLogs, "access-logs", false, "Include access logs section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportConfigJSON, "config-json", false, "Include raw config JSON")
	CatalogReportCmd.Flags().StringVar(&catalogReportSettings, "settings", "", "Path to report settings YAML file")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAudit, "audit", false, "Append an audit page with settings, build info, queries, and scraper provenance")
	CatalogReportCmd.Flags().IntVar(&catalogReportStaleDays, "stale-days", 0, "Days since last sign-in before access is flagged stale (overrides settings; default 90)")
	CatalogReportCmd.Flags().IntVar(&catalogReportReviewOverdueDays, "review-overdue-days", 0, "Days since last review before access is flagged overdue (overrides settings; default 90)")
	CatalogReportCmd.Flags().StringArrayVar(&catalogReportFilters, "filter", nil, "Extra query filter (repeatable, appended to settings.filters)")

	clicky.RegisterSubCommand("catalog", CatalogReportCmd)
}

// expandDescendants loads every descendant of the given configs via
// duty's parent-path expansion (lookup_config_children SQL) and returns the
// union — matched configs first, then descendants in load order — deduped
// by ID. The limit is applied to the final slice so the caller's cap is
// respected.
func expandDescendants(ctx context.Context, configs []models.ConfigItem, limit int) ([]models.ConfigItem, error) {
	if len(configs) == 0 {
		return configs, nil
	}

	seen := make(map[uuid.UUID]bool, len(configs))
	ids := make([]uuid.UUID, 0, len(configs))
	for _, c := range configs {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		ids = append(ids, c.ID)
	}

	expanded, err := query.ExpandConfigChildren(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to expand descendants: %w", err)
	}

	// Keep the original matched configs at the front to preserve the
	// existing semantics (report.ConfigItem = configs[0]).
	out := append([]models.ConfigItem{}, configs...)
	missing := make([]uuid.UUID, 0, len(expanded))
	for _, id := range expanded {
		if seen[id] {
			continue
		}
		seen[id] = true
		missing = append(missing, id)
	}
	if len(missing) > 0 {
		loaded, err := query.GetConfigsByIDs(ctx, missing)
		if err != nil {
			return nil, fmt.Errorf("failed to load descendant configs: %w", err)
		}
		out = append(out, loaded...)
	}

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
