package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/shutdown"
	"github.com/spf13/cobra"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/views"
)

var ViewCmd = &cobra.Command{
	Use: "view",
}

var (
	viewFormat  string
	viewOutFile string
	viewVars    []string
)

var ViewRun = &cobra.Command{
	Use:              "run <view.yaml> [--var key=value ...]",
	Short:            "Execute a View CRD from a YAML file and export results",
	Args:             cobra.ExactArgs(1),
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

		manifest, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", args[0], err)
		}

		var allViews []v1.View
		for _, m := range strings.Split(string(manifest), "---") {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			var view v1.View
			if err := yamlutil.Unmarshal([]byte(m), &view); err != nil {
				return fmt.Errorf("failed to parse view YAML: %w", err)
			}
			if view.Name != "" {
				allViews = append(allViews, view)
			}
		}

		if len(allViews) == 0 {
			return fmt.Errorf("no views found in %s", args[0])
		}

		vars := parseVarFlags(viewVars)

		data, err := views.ExportMulti(ctx, allViews, vars, viewFormat, nil)
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
			return err
		}

		out := viewOutFile
		if out == "" {
			out = "stdout"
		}
		logger.Infof("Rendering %d view(s) to %s (%s) %dKB", len(allViews), out, viewFormat, len(data)/1024)

		if viewOutFile != "" {
			if err := os.WriteFile(viewOutFile, data, 0600); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		} else {
			fmt.Print(string(data))
		}

		return nil
	},
}

func parseVarFlags(flags []string) map[string]string {
	vars := make(map[string]string)
	for _, f := range flags {
		k, v, ok := strings.Cut(f, "=")
		if ok {
			vars[k] = v
		}
	}
	return vars
}

func init() {
	ViewRun.Flags().StringVarP(&viewFormat, "format", "f", "json", "Output format: json, csv, html, pdf, facet-html, facet-pdf")
	ViewRun.Flags().StringVarP(&viewOutFile, "out-file", "o", "", "Write output to file instead of stdout")
	ViewRun.Flags().StringSliceVar(&viewVars, "var", nil, "Template variables as key=value pairs")
	ViewCmd.AddCommand(ViewRun)
}
