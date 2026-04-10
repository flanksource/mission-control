package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/shutdown"
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

	catalogReportChanges         bool
	catalogReportInsights        bool
	catalogReportRelationships   bool
	catalogReportAccess          bool
	catalogReportAccessLogs      bool
	catalogReportConfigJSON      bool
	catalogReportRecursive       bool
	catalogReportGroupBy         string
	catalogReportChangeArtifacts bool
	catalogReportAudit           bool
	catalogReportLimit           int
	catalogReportMaxItems        int
	catalogReportMaxChanges      int
)

var CatalogReportCmd = &cobra.Command{
	Use:   "report <ID|QUERY>",
	Short: "Generate a catalog report for a config item",
	Long: `Generate a PDF/HTML report for a config item including changes, insights,
relationships, RBAC access, and access logs.

Examples:
  # By config ID
  catalog report 018f4e6a-1234-5678-9abc-def012345678

  # By query
  catalog report type=Kubernetes::Namespace name=default

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

		opts := buildCatalogReportOptions()

		queryArgs := args
		if opts.Settings != nil {
			if fq := opts.Settings.FilterQuery(); fq != "" {
				queryArgs = append(queryArgs, fq)
			}
		}

		configs, err := resolveConfigs(ctx, queryArgs, catalogReportLimit)
		if err != nil {
			return err
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

func buildCatalogReportOptions() catalog.Options {
	opts := catalog.Options{
		Title:           catalogReportTitle,
		Recursive:       catalogReportRecursive,
		GroupBy:         catalogReportGroupBy,
		ChangeArtifacts: catalogReportChangeArtifacts,
		Audit:           catalogReportAudit,
		Limit:           catalogReportLimit,
		MaxItems:        catalogReportMaxItems,
		MaxChanges:      catalogReportMaxChanges,
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
		if d, err := duration.ParseDuration(catalogReportSince); err == nil {
			opts.Since = time.Duration(d)
		}
	}

	settings, settingsSource, err := catalog.ResolveSettings(catalogReportSettings)
	if err != nil {
		logger.Fatalf("failed to load settings: %v", err)
	}
	opts.Settings = settings
	opts.SettingsPath = settingsSource

	return opts
}

func init() {
	CatalogReportCmd.Flags().StringVarP(&catalogReportFormat, "format", "f", "facet-pdf", "Output format: json, facet-html, facet-pdf")
	CatalogReportCmd.Flags().StringVarP(&catalogReportOutFile, "out-file", "o", "", "Write output to file instead of stdout")
	CatalogReportCmd.Flags().StringVar(&catalogReportSince, "since", "30d", "Time range for changes and access logs (supports d/w/y e.g. 7d, 2w, 30d)")
	CatalogReportCmd.Flags().StringVar(&catalogReportTitle, "title", "", "Report title (default auto-generated)")
	CatalogReportCmd.Flags().StringVar(&report.SourceDir, "report-source", "", "Local directory or TSX file for report rendering (overrides embedded reports)")

	CatalogReportCmd.Flags().BoolVar(&catalogReportRecursive, "recursive", false, "Include all descendant config items")
	CatalogReportCmd.Flags().StringVar(&catalogReportGroupBy, "group-by", "merged", "Group descendant data: 'merged' or 'config'")
	CatalogReportCmd.Flags().BoolVar(&catalogReportChangeArtifacts, "change-artifacts", false, "Embed change artifacts (images/screenshots) in the report")
	CatalogReportCmd.Flags().IntVar(&catalogReportLimit, "limit", 50, "Maximum number of config items to report on, including recursive descendants (0 = unlimited)")
	CatalogReportCmd.Flags().IntVar(&catalogReportMaxItems, "max-items", 50, "Maximum items per section (changes, analyses, access, access-logs). Section-specific flags override this. (0 = unlimited)")
	CatalogReportCmd.Flags().IntVar(&catalogReportMaxChanges, "max-changes", 100, "Maximum changes per entry, overrides --max-items for the changes section (0 = unlimited)")
	CatalogReportCmd.Flags().BoolVar(&catalogReportChanges, "changes", true, "Include config changes section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportInsights, "insights", true, "Include config insights section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportRelationships, "relationships", true, "Include relationships section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAccess, "access", true, "Include RBAC access section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAccessLogs, "access-logs", true, "Include access logs section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportConfigJSON, "config-json", false, "Include raw config JSON")
	CatalogReportCmd.Flags().StringVar(&catalogReportSettings, "settings", "", "Path to report settings YAML file")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAudit, "audit", false, "Append an audit page with settings, build info, queries, and scraper provenance")

	Catalog.AddCommand(CatalogReportCmd)
}
