package cmd

import (
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/spf13/cobra"
)

func parseQuery(args []string) query.SearchResourcesRequest {
	logger.Infof("Search query %v", args)
	return query.SearchResourcesRequest{
		Limit:   100,
		Configs: []types.ResourceSelector{{Cache: "no-cache", Search: strings.Join(args, " ")}},
	}
}

// Query is a thin alias for `catalog list --query <joined args>`. It exists
// to preserve the existing `catalog query <Q>` ergonomics now that list is
// the primary entrypoint. We call listConfigs directly rather than re-
// entering cobra to avoid re-running persistent pre-runs.
var Query = &cobra.Command{
	Use:              "query <QUERY>",
	Short:            "Alias for `catalog list --query <QUERY>`",
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		configs, err := listConfigs(catalogListOpts{
			Query: strings.Join(args, " "),
		})
		if err != nil {
			return err
		}
		clicky.MustPrint(configs, clicky.Flags.FormatOptions)
		return nil
	},
}

var Mock = &cobra.Command{
	Use:              "mock",
	Short:            "Load the database with mock data from the duty dummy fixture",
	PersistentPreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			logger.Fatalf(err.Error())
			return
		}
		defer stop()

		base := dummy.GetStaticDummyData(ctx.DB())
		if err := base.Populate(ctx); err != nil {
			logger.Fatalf("Failed to populate base dummy data: %v", err)
			return
		}

		app := dummy.GetAllApplicationDummyData()
		if err := app.Delete(ctx.DB()); err != nil {
			logger.Warnf("Failed to delete existing application dummy data: %v", err)
		}
		if err := app.Populate(ctx); err != nil {
			logger.Fatalf("Failed to populate application dummy data: %v", err)
			return
		}

		logger.Infof("Mock data loaded successfully")
	},
}

func init() {
	clicky.BindAllFlags(Query.PersistentFlags(), "format")
	clicky.RegisterSubCommand("catalog", Query)
	clicky.RegisterSubCommand("catalog", Mock)
}
