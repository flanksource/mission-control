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
	"github.com/flanksource/incident-commander/catalog_report"
	"github.com/flanksource/incident-commander/report"
)

var (
	catalogReportFormat  string
	catalogReportOutFile string
	catalogReportSince   string
	catalogReportTitle   string

	catalogReportChanges         bool
	catalogReportInsights        bool
	catalogReportRelationships   bool
	catalogReportAccess          bool
	catalogReportAccessLogs      bool
	catalogReportConfigJSON      bool
	catalogReportRecursive       bool
	catalogReportGroupBy         string
	catalogReportChangeArtifacts bool
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

		configs, err := resolveConfigs(ctx, args, 0)
		if err != nil {
			return err
		}

		opts := buildCatalogReportOptions()

		data, err := catalog_report.Export(ctx, configs, opts, catalogReportFormat)
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
			return err
		}

		out := catalogReportOutFile
		if out == "" {
			out = "stdout"
		}
		logger.Infof("Rendering catalog report to %s (%s) %dKB", out, catalogReportFormat, len(data)/1024)

		if catalogReportOutFile != "" {
			if err := os.WriteFile(catalogReportOutFile, data, 0600); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		} else {
			fmt.Print(string(data))
		}

		return nil
	},
}

func buildCatalogReportOptions() catalog_report.Options {
	opts := catalog_report.Options{
		Title:           catalogReportTitle,
		Recursive:       catalogReportRecursive,
		GroupBy:         catalogReportGroupBy,
		ChangeArtifacts: catalogReportChangeArtifacts,
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
	CatalogReportCmd.Flags().BoolVar(&catalogReportChanges, "changes", true, "Include config changes section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportInsights, "insights", true, "Include config insights section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportRelationships, "relationships", true, "Include relationships section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAccess, "access", true, "Include RBAC access section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportAccessLogs, "access-logs", true, "Include access logs section")
	CatalogReportCmd.Flags().BoolVar(&catalogReportConfigJSON, "config-json", false, "Include raw config JSON")

	Catalog.AddCommand(CatalogReportCmd)
}
