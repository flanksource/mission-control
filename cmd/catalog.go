package cmd

import (
	"os"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/spf13/cobra"
)

var catalogOutfile string
var catalogWaitFor time.Duration

var Catalog = &cobra.Command{
	Use: "catalog",
}

func parseQuery(args []string) query.SearchResourcesRequest {
	logger.Infof("Search query %v", args)
	return query.SearchResourcesRequest{
		Limit:   100,
		Configs: []types.ResourceSelector{{Cache: "no-cache", Search: strings.Join(args, " ")}},
	}
}

var Query = &cobra.Command{
	Use:              "query <QUERY>",
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, _, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			logger.Fatalf(err.Error())
			return
		}

		req := parseQuery(args)
		logger.Debugf("Search Request: %s", logger.Pretty(req))
		start := time.Now()
		var response *query.SearchResourcesResponse

		response, err = query.SearchResources(ctx, req)
		if err != nil {
			logger.Fatalf(err.Error())
			os.Exit(1)
		}

		for time.Since(start) < catalogWaitFor {
			if len(response.Configs) > 0 || len(response.Components) > 0 || len(response.Checks) > 0 {
				break
			}

			response, err = query.SearchResources(ctx, req)
			if err != nil {
				logger.Fatalf(err.Error())
				os.Exit(1)
			}

			logger.Infof("Waiting %s for %s", req, catalogWaitFor-time.Since(start))
			time.Sleep(1 * time.Second)
		}

		if catalogOutfile != "" {
			logger.Infof("Writing output to %s", catalogOutfile)
			if err := clicky.FormatToFile(*response, clicky.Flags.FormatOptions, catalogOutfile); err != nil {
				logger.Fatalf(err.Error())
				os.Exit(1)
			}
		} else {
			clicky.MustPrint(*response, clicky.Flags.FormatOptions)
		}
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
	Query.Flags().StringVarP(&catalogOutfile, "out-file", "o", "", "Write catalog output to a file instead of stdout")
	Query.Flags().DurationVarP(&catalogWaitFor, "wait", "w", 60*time.Second, "Wait for this long for resources to be discovered")
	clicky.BindAllFlags(Query.PersistentFlags(), "format")

	Get.Flags().StringVar(&catalogGetSince, "since", "7d", "Time range for changes (supports d/w/y e.g. 7d, 2w, 30d)")
	clicky.BindAllFlags(Get.PersistentFlags(), "format")

	Catalog.AddCommand(Query)
	Catalog.AddCommand(Mock)
	Catalog.AddCommand(Get)
	Root.AddCommand(Catalog)
}
