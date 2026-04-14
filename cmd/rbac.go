package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/duty/types"
	"github.com/spf13/cobra"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac_report"
	"github.com/flanksource/incident-commander/report"
)

var RBACCmd = &cobra.Command{
	Use: "rbac",
}

var (
	rbacFormat     string
	rbacOutFile    string
	rbacStaleDays  int
	rbacReviewDays int
	rbacSince      string
	rbacTitle      string
	rbacView       string
)

var ExportRBAC = &cobra.Command{
	Use:   "export [application.yaml | config-query...]",
	Short: "Generate an RBAC report for config items",
	Long: `Generate an RBAC report from an application YAML or a config query.

With no arguments, exports all configs that have RBAC access entries.

Examples:
  # All configs with access entries
  rbac export

  # From an application YAML
  rbac export application.yaml

  # From a config query (same key=value syntax as catalog query)
  rbac export type=Azure::EnterpriseApplication
  rbac export type=Kubernetes::Namespace name=default

  # Bare search term (resolved via ResourceSelector.Search)
  rbac export nginx`,
	Args:             cobra.ArbitraryArgs,
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

		opts := buildRBACOptions(args)

		data, err := rbac_report.Export(ctx, opts, rbacFormat)
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
			return err
		}

		out := rbacOutFile
		if out == "" {
			out = "stdout"
		}
		logger.Infof("Rendering rbac to %s (%s) %dKB", out, rbacFormat, len(data)/1024)

		if rbacOutFile != "" {
			if err := os.WriteFile(rbacOutFile, data, 0600); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		} else {
			fmt.Print(string(data))
		}

		return nil
	},
}

func buildRBACOptions(args []string) rbac_report.Options {
	opts := rbac_report.Options{
		Title:             rbacTitle,
		StaleDays:         rbacStaleDays,
		ReviewOverdueDays: rbacReviewDays,
		View:              rbacView,
	}

	if rbacSince != "" {
		if d, err := time.ParseDuration(rbacSince); err == nil {
			opts.ChangelogSince = d
		}
	}

	if len(args) == 0 {
		return opts
	}

	if isYAMLFile(args[0]) {
		opts.Selectors = selectorsFromAppYAML(args[0])
	} else {
		opts.Selectors = []types.ResourceSelector{parseConfigQuery(args)}
	}

	return opts
}

func isYAMLFile(arg string) bool {
	return strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml")
}

func parseConfigQuery(args []string) types.ResourceSelector {
	selector := types.ResourceSelector{
		Cache:  "no-cache",
		Search: strings.Join(args, " "),
	}
	return selector
}

func selectorsFromAppYAML(path string) []types.ResourceSelector {
	manifest, err := os.ReadFile(path)
	if err != nil {
		logger.Warnf("failed to read %s: %v", path, err)
		return nil
	}

	var app v1.Application
	for _, m := range strings.Split(string(manifest), "---") {
		if err := yamlutil.Unmarshal([]byte(m), &app); err != nil {
			logger.Warnf("failed to parse application YAML: %v", err)
			return nil
		}
		if app.Name != "" {
			break
		}
	}

	if len(app.Spec.Mapping.Logins) > 0 {
		return app.Spec.Mapping.Logins
	}

	return app.AllSelectors()
}

func init() {
	ExportRBAC.Flags().StringVarP(&rbacFormat, "format", "f", "json", "Output format: json, csv, facet-html, facet-pdf")
	ExportRBAC.Flags().StringVarP(&rbacOutFile, "out-file", "o", "", "Write output to file instead of stdout")
	ExportRBAC.Flags().IntVar(&rbacStaleDays, "stale-days", 90, "Days without sign-in before access is flagged stale")
	ExportRBAC.Flags().IntVar(&rbacReviewDays, "review-days", 90, "Days without review before access is flagged overdue")
	ExportRBAC.Flags().StringVar(&rbacSince, "since", "2160h", "Changelog time range (Go duration, default 90 days)")
	ExportRBAC.Flags().StringVar(&rbacTitle, "title", "", "Report title (default auto-generated)")
	ExportRBAC.Flags().StringVar(&rbacView, "view", "resource", "Report view: resource, user, or matrix")
	ExportRBAC.Flags().StringVar(&report.SourceDir, "report-source", "", "Local directory or TSX file for report rendering (overrides embedded reports)")
	RBACCmd.AddCommand(ExportRBAC)
}
