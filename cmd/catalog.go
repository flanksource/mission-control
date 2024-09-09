package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/auth"
	"github.com/spf13/cobra"
)

var catalogOutfile string
var catalogOutformat string
var catalogWaitFor time.Duration

var Catalog = &cobra.Command{
	Use: "catalog",
}

func parseQuery(args []string) query.SearchResourcesRequest {
	request := query.SearchResourcesRequest{}
	tags := make(map[string]string)
	var configTypes []string
	for _, arg := range args[1:] {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			logger.Warnf("Invalid param: %s", arg)
			continue
		}

		switch parts[0] {
		case "config":
			request.Configs = append(request.Configs, types.ResourceSelector{Search: parts[1]})
		case "config_id":
			request.Configs = append(request.Configs, types.ResourceSelector{ID: parts[1]})
		case "component":
			request.Components = append(request.Components, types.ResourceSelector{Search: parts[1]})
		case "component_id":
			request.Components = append(request.Components, types.ResourceSelector{ID: parts[1]})
		case "check":
			request.Checks = append(request.Checks, types.ResourceSelector{Search: parts[1]})
		case "check_id":
			request.Checks = append(request.Checks, types.ResourceSelector{ID: parts[1]})
		case "type":
			configTypes = append(configTypes, parts[1])
		default:
			tags[parts[0]] = parts[1]
		}

	}
	if len(configTypes) > 0 {
		for i := range request.Configs {
			request.Configs[i].Types = configTypes
		}
		for i := range request.Components {
			request.Components[i].Types = configTypes
		}
	}
	if len(tags) > 0 {
		for i := range request.Configs {
			for k, v := range tags {
				request.Configs[i].LabelSelector += fmt.Sprintf(" %s=%s", k, v)
			}
		}
		for i := range request.Components {
			for k, v := range tags {
				request.Components[i].LabelSelector += fmt.Sprintf(" %s=%s", k, v)
			}
		}
	}

	return request
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

		ctx.DB().Begin()
		response, err = query.SearchResources(ctx, req)
		if err != nil {
			logger.Fatalf(err.Error())
			os.Exit(1)
		}
		ctx.DB().Commit()

		for time.Since(start) < catalogWaitFor {

			if len(response.Configs) > 0 || len(response.Components) > 0 || len(response.Checks) > 0 {
				break
			}
			ctx.DB().Begin()
			response, err = query.SearchResources(ctx, req)
			ctx.DB().Commit()

			if err != nil {
				logger.Fatalf(err.Error())
				os.Exit(1)
			}

			logger.Infof("Waiting %s for resources to be discovered...", catalogWaitFor-time.Since(start))
			time.Sleep(3 * time.Second)

		}

		saveOutput(response, catalogOutfile, catalogOutformat)

		ctx = ctx.WithUser(auth.GetSystemUser(&ctx))

	},
}

func init() {
	Query.Flags().StringVarP(&catalogOutfile, "out-file", "o", "", "Write catalog output to a file instead of stdout")
	Query.Flags().StringVarP(&catalogOutformat, "out-format", "f", "yaml", "Format of output file or stdout (yaml or json)")
	Query.Flags().DurationVarP(&catalogWaitFor, "wait", "w", 60*time.Second, "Wait for this long for resources to be discovered")
	Catalog.AddCommand(Query)
	Root.AddCommand(Catalog)
}
