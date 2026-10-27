package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/shutdown"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	k8sTypes "k8s.io/apimachinery/pkg/types"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/application"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/report"
)

var ApplicationCmd = &cobra.Command{
	Use: "application",
}

var exportFormat string
var exportOutfile string
var exportAs string

var ExportApplication = &cobra.Command{
	Use:              "export <application.yaml>",
	Args:             cobra.ExactArgs(1),
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf("%s", err.Error())
		}

		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			return err
		}
		shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)
		shutdown.WaitForSignal()
		applyFacetFlags(ctx)

		person, err := db.FindPerson(ctx, exportAs)
		if err != nil {
			return fmt.Errorf("failed to find person %q: %w", exportAs, err)
		}
		if err := dutyRBAC.Init(ctx, nil, adapter.NewPermissionAdapter); err != nil {
			return fmt.Errorf("failed to initialize rbac: %w", err)
		}
		ctx = ctx.WithUser(person)

		manifest, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", args[0], err)
		}

		var app v1.Application
		for _, m := range strings.Split(string(manifest), "---") {
			if err := yamlutil.Unmarshal([]byte(m), &app); err != nil {
				return fmt.Errorf("failed to parse application YAML: %w", err)
			}
			if app.Name != "" {
				break
			}
		}
		if app.Name == "" {
			return fmt.Errorf("no application name found in %s", args[0])
		}

		if app.UID == "" {
			app.UID = k8sTypes.UID(uuid.New().String())
		}

		if err := db.PersistApplicationFromCRD(ctx, &app); err != nil {
			return fmt.Errorf("failed to persist application: %w", err)
		}

		data, err := application.Export(ctx, app.Namespace, app.Name, exportFormat)
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
			return err
		}

		if exportOutfile != "" {
			if err := os.WriteFile(exportOutfile, data, 0600); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		} else {
			fmt.Print(string(data))
		}

		return nil
	},
}

func init() {
	ExportApplication.Flags().StringVarP(&exportFormat, "format", "f", "json", "Output format: json, html, pdf, facet-html, facet-pdf")
	ExportApplication.Flags().StringVarP(&exportOutfile, "out-file", "o", "", "Write output to file instead of stdout")
	ExportApplication.Flags().StringVar(&exportAs, "as", "", "Email or ID of the person to export as. The export only includes what they may read")
	_ = ExportApplication.MarkFlagRequired("as")
	ExportApplication.Flags().StringVar(&report.SourceDir, "report-source", "", "Local directory or TSX file for report rendering (overrides embedded reports)")
	addFacetFlags(ExportApplication)
	ApplicationCmd.AddCommand(ExportApplication)
}
