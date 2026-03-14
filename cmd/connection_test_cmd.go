package cmd

import (
	gocontext "context"
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/connection"
	"github.com/flanksource/incident-commander/db"
)

var ConnectionTest = &cobra.Command{
	Use:   "test",
	Short: "Test a connection without saving",
	Long: `Test a connection using CLI flags (same as 'add') or from a Connection CRD YAML file.

Examples:
  # Test using CLI flags (same flags as 'connection add')
  app connection test postgres --name mydb --url "postgres://user:pass@localhost:5432/db"

  # Test from a CRD YAML file
  app connection test -f connection.yaml`,
	PersistentPreRun:  PreRun,
	SilenceUsage:      true,
	DisableAutoGenTag: true,
}

var connectionTestFile string

func hydrateAndTest(conn *models.Connection) (any, error) {
	ctx := context.NewContext(gocontext.Background())
	hydrated, err := ctx.HydrateConnection(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	}

	result, err := connection.Test(ctx, hydrated)
	if err != nil {
		return result, fmt.Errorf("connection test failed: %w", err)
	}

	return result, nil
}

func runConnectionTest(flags *connectionFlags) (any, error) {
	if flags.FromProfile != "" {
		if err := loadAWSProfile(flags); err != nil {
			return nil, err
		}
	}

	conn, err := buildConnectionFromFlags(flags)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection: %w", err)
	}

	return hydrateAndTest(&conn)
}

func runConnectionTestFromFile(filename string) (any, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var crd v1.Connection
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if crd.Kind != "" && crd.Kind != "Connection" {
		return nil, fmt.Errorf("expected Kind=Connection, got %s", crd.Kind)
	}

	conn, err := db.ConnectionFromCRD(&crd)
	if err != nil {
		return nil, err
	}

	return hydrateAndTest(&conn)
}

func newConnectionTestTypeCommand(spec connectionTypeSpec) *cobra.Command {
	flags := &connectionFlags{}
	cmd := &cobra.Command{
		Use:               spec.Name,
		Aliases:           spec.Aliases,
		Short:             "Test a " + spec.Name + " connection",
		DisableAutoGenTag: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.Type = spec.Type
			flags.Name = "test"
			flags.Namespace = "default"
			result, err := runConnectionTest(flags)
			if err != nil {
				if result != nil {
					clicky.MustPrint(result, clicky.Flags.FormatOptions)
				}
				return err
			}
			clicky.MustPrint(result, clicky.Flags.FormatOptions)
			return nil
		},
	}
	addTypeSpecificFlags(cmd, flags, spec.Type)
	return cmd
}

func init() {
	clicky.BindAllFlags(ConnectionTest.PersistentFlags(), "format")

	ConnectionTest.Flags().StringVarP(&connectionTestFile, "file", "f", "", "Connection CRD YAML file")

	ConnectionTest.RunE = func(cmd *cobra.Command, args []string) error {
		if connectionTestFile != "" {
			result, err := runConnectionTestFromFile(connectionTestFile)
			if err != nil {
				if result != nil {
					clicky.MustPrint(result, clicky.Flags.FormatOptions)
				}
				return err
			}
			clicky.MustPrint(result, clicky.Flags.FormatOptions)
			return nil
		}
		return fmt.Errorf("specify a connection type subcommand or use -f <file.yaml>")
	}

	for _, spec := range connectionAddTypeSpecs {
		ConnectionTest.AddCommand(newConnectionTestTypeCommand(spec))
	}

	Connection.AddCommand(ConnectionTest)
}
