package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/spf13/cobra"
)

var catalogOutfile string
var catalogOutformat string
var catalogWaitFor time.Duration

var Catalog = &cobra.Command{
	Use: "catalog",
}

func parseQuery(args []string) query.SearchResourcesRequest {
	logger.Infof("Search query %v", args)
	request := query.SearchResourcesRequest{
		Limit: 5,
	}
	tags := make(map[string]string)
	selector := types.ResourceSelector{
		Cache: "no-cache",
	}
	for _, arg := range args {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			logger.Warnf("Invalid param: %s", arg)
			continue
		}

		switch parts[0] {
		case "limit":
			l, _ := strconv.Atoi(parts[1])
			request.Limit = l
		case "search":
			selector.Search = parts[1]
		case "scope":
			selector.Scope = parts[1]
		case "type":
			selector.Types = append(selector.Types, parts[1])
		case "name":
			selector.Name = parts[1]
		case "namespace":
			selector.Namespace = parts[1]
		case "id":
			selector.ID = parts[1]
		case "status":
			selector.Statuses = append(selector.Statuses, parts[1])
		default:
			tags[parts[0]] = parts[1]
		}
	}

	for k, v := range tags {
		if strings.HasPrefix(k, "@") {
			selector.TagSelector += fmt.Sprintf(" %s=%s", k[1:], v)
		} else {
			selector.LabelSelector += fmt.Sprintf(" %s=%s", k, v)
		}
	}
	request.Configs = []types.ResourceSelector{selector}

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

			logger.Infof("Waiting %s for %s", req, catalogWaitFor-time.Since(start))
			time.Sleep(3 * time.Second)
		}

		saveOutput(response, catalogOutfile, catalogOutformat)
	},
}

func init() {
	Query.Flags().StringVarP(&catalogOutfile, "out-file", "o", "", "Write catalog output to a file instead of stdout")
	Query.Flags().StringVarP(&catalogOutformat, "out-format", "f", "json", "Format of output file or stdout (yaml or json)")
	Query.Flags().DurationVarP(&catalogWaitFor, "wait", "w", 60*time.Second, "Wait for this long for resources to be discovered")
	Catalog.AddCommand(Query)
	Root.AddCommand(Catalog)
}
