package cmd

import (
	gocontext "context"
	"errors"
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/connection"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/sdk"
)

var ConnectionTest = &cobra.Command{
	Use:   "test",
	Short: "Test a connection without saving",
	Long: `Test a connection using CLI flags, from a CRD YAML file, or by loading from the database.

Examples:
  # Test using CLI flags (same flags as 'connection add')
  app connection test postgres --url "postgres://user:pass@localhost:5432/db"

  # Test from a CRD YAML file
  app connection test -f connection.yaml

  # Test an existing connection from the database
  app connection test --name mydb --namespace default

  # Test an existing connection with URL override
  app connection test http --name my-http --url https://other-endpoint.com`,
	PersistentPreRun:  PreRun,
	SilenceUsage:      true,
	DisableAutoGenTag: true,
}

var (
	connectionTestFile      string
	connectionTestName      string
	connectionTestNamespace string
)

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

func runConnectionTestFromDB(name, namespace string, overrides *connectionFlags) (any, error) {
	if mcCtx, ok := contextHasAPI(); ok {
		return runConnectionTestViaAPI(mcCtx, name, namespace)
	}

	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return nil, err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

	var conn models.Connection
	if err := ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).First(&conn).Error; err != nil {
		return nil, fmt.Errorf("connection %s/%s not found: %w", namespace, name, err)
	}

	if overrides != nil {
		applyConnectionOverrides(&conn, overrides)
	}

	if clicky.Flags.LevelCount >= 1 {
		printConnectionState(conn, clicky.Flags.LevelCount)
	}

	hydrated, err := ctx.HydrateConnection(&conn)
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	}

	result, err := connection.Test(ctx, hydrated)
	if err != nil {
		return result, fmt.Errorf("connection test failed: %w", err)
	}

	return result, nil
}

func runConnectionTestViaAPI(mcCtx *MCContext, name, namespace string) (any, error) {
	result, err := callConnectionTestAPI(mcCtx, name, namespace)
	if !errors.Is(err, sdk.ErrHTMLResponse) {
		return result, err
	}

	upgraded, upErr := ensureAPIBase(mcCtx)
	if upErr != nil {
		return nil, fmt.Errorf("%w (probe failed: %v)", err, upErr)
	}
	if !upgraded {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "Upgraded context %q server to %s\n", mcCtx.Name, mcCtx.Server)
	return callConnectionTestAPI(mcCtx, name, namespace)
}

func callConnectionTestAPI(mcCtx *MCContext, name, namespace string) (any, error) {
	client := sdk.New(mcCtx.Server, mcCtx.Token)

	conn, err := client.GetConnection(name, namespace)
	if err != nil {
		return nil, err
	}

	result, err := client.TestConnection(conn.ID.String())
	if err != nil {
		return nil, err
	}
	return result.Payload, nil
}

func applyConnectionOverrides(conn *models.Connection, flags *connectionFlags) {
	if flags.URL != "" {
		conn.URL = flags.URL
	}
	if flags.Username != "" {
		conn.Username = flags.Username
	}
	if flags.Password != "" {
		conn.Password = flags.Password
	}
	if flags.Certificate != "" {
		conn.Certificate = flags.Certificate
	}
	if flags.InsecureTLS {
		conn.InsecureTLS = true
	}
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
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.Type = spec.Type
			if flags.Name != "" {
				result, err := runConnectionTestFromDB(flags.Name, flags.Namespace, flags)
				if err != nil {
					if result != nil {
						clicky.MustPrint(result, clicky.Flags.FormatOptions)
					}
					return err
				}
				clicky.MustPrint(result, clicky.Flags.FormatOptions)
				return nil
			}
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
	cmd.Flags().StringVar(&flags.Name, "name", "", "Connection name (load from database, overrides applied from other flags)")
	cmd.Flags().StringVar(&flags.Namespace, "namespace", "default", "Connection namespace")
	addTypeSpecificFlags(cmd, flags, spec.Type)
	return cmd
}

func init() {
	clicky.BindAllFlags(ConnectionTest.PersistentFlags(), "format")

	ConnectionTest.Flags().StringVarP(&connectionTestFile, "file", "f", "", "Connection CRD YAML file")
	ConnectionTest.Flags().StringVar(&connectionTestName, "name", "", "Connection name (load from database)")
	ConnectionTest.Flags().StringVar(&connectionTestNamespace, "namespace", "default", "Connection namespace (used with --name)")

	ConnectionTest.RunE = func(cmd *cobra.Command, args []string) error {
		if connectionTestName != "" {
			result, err := runConnectionTestFromDB(connectionTestName, connectionTestNamespace, nil)
			if err != nil {
				if result != nil {
					clicky.MustPrint(result, clicky.Flags.FormatOptions)
				}
				return err
			}
			clicky.MustPrint(result, clicky.Flags.FormatOptions)
			return nil
		}
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
		return fmt.Errorf("specify a connection type subcommand, --name, or -f <file.yaml>")
	}

	for _, spec := range connectionAddTypeSpecs {
		ConnectionTest.AddCommand(newConnectionTestTypeCommand(spec))
	}

	Connection.AddCommand(ConnectionTest)
}
