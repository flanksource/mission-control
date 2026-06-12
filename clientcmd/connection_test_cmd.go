package clientcmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/spf13/cobra"

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
	SilenceUsage:      true,
	DisableAutoGenTag: true,
}

var (
	connectionTestFile      string
	connectionTestName      string
	connectionTestNamespace string
)

// runConnectionTestFromDB tests a saved connection. With an API context it runs
// remotely; otherwise it delegates to the local DB implementation when present.
func runConnectionTestFromDB(name, namespace string, overrides *ConnectionFlags) (any, error) {
	if mcCtx, ok := ContextHasAPI(); ok {
		return runConnectionTestViaAPI(mcCtx, name, namespace)
	}
	if LocalConnections == nil {
		return nil, errNoLocalConnections
	}
	return LocalConnections.TestSaved(name, namespace, overrides)
}

func runConnectionTestViaAPI(mcCtx *MCContext, name, namespace string) (any, error) {
	result, err := callConnectionTestAPI(mcCtx, name, namespace)
	if !errors.Is(err, sdk.ErrHTMLResponse) {
		return result, err
	}

	upgraded, upErr := EnsureAPIBase(mcCtx)
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
	client := NewAPIClient(mcCtx)

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

// runConnectionTestTransient tests a connection built from flags (not saved).
// This requires local hydration, so it is only available in the full binary.
func runConnectionTestTransient(flags *ConnectionFlags) (any, error) {
	if LocalConnections == nil {
		return nil, fmt.Errorf("testing an unsaved connection %w; use --name to test a saved connection remotely", errNoLocalConnections)
	}
	return LocalConnections.TestTransient(flags)
}

func newConnectionTestTypeCommand(spec connectionTypeSpec) *cobra.Command {
	flags := &ConnectionFlags{}
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
			result, err := runConnectionTestTransient(flags)
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
			if LocalConnections == nil {
				return fmt.Errorf("testing from a CRD file %w", errNoLocalConnections)
			}
			result, err := LocalConnections.TestFile(connectionTestFile)
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
