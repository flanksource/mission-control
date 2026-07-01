package main

import "github.com/spf13/cobra"

func documentCatalogCommand(catalogCmd *cobra.Command) {
	if catalogCmd == nil {
		return
	}

	disableCatalogRootList(catalogCmd)

	catalogCmd.Short = "List and inspect catalog resources"
	catalogCmd.Long = `List and inspect catalog resources from the current Mission Control context.

The catalog contains configuration items discovered from Kubernetes, cloud,
Git, CI/CD, and other connected systems.

Choose a subcommand for the workflow you want. Use "list" for simple flag-based
filters. Use "search" when you want the catalog query language in one expression.`
	catalogCmd.Example = `  faro catalog list --limit 50
  faro catalog list --type Kubernetes::Pod --namespace default
  faro catalog list --tag cluster=prod --agent all --limit 50
  faro catalog search 'type=Kubernetes::Pod tags.cluster=prod name=api*'
  faro catalog get <config-id>`

	if listCmd := catalogSubcommand(catalogCmd, "list"); listCmd != nil {
		listCmd.Short = "List catalog resources using filter flags"
		listCmd.Long = `List catalog resources using simple, structured filter flags.

This command returns lightweight catalog rows.

Use --query for free-form text or a catalog query expression, --type for config
types, --namespace for a namespace, --tag for tag label selectors, --agent to
select an agent, and --limit to control result count.

Use "faro catalog search <QUERY>" when the query expression is the main input
and you do not need the other list filter flags.`
		listCmd.Example = `  faro catalog list --limit 50
  faro catalog list --type Kubernetes::Pod --namespace default
  faro catalog list --tag cluster=prod --tag env=dev --agent all --limit 50
  faro catalog list --query api
  faro catalog list --type Kubernetes::Deployment`
	}

	if getCmd := catalogSubcommand(catalogCmd, "get"); getCmd != nil {
		getCmd.Short = "Get a catalog resource by ID"
		getCmd.Long = `Get a single catalog resource by ID.

Use "faro catalog list" or "faro catalog search" to find resource IDs first.

By default this returns the catalog item. Use --relationships to return the
incoming and outgoing relationship tree instead.`
		getCmd.Example = `  faro catalog get 018f4e6a-1234-5678-9abc-def012345678
  faro catalog get <config-id> --relationships
  faro catalog search 'name=api*' --limit 10`
	}

	if searchCmd := catalogSubcommand(catalogCmd, "search"); searchCmd != nil {
		searchCmd.Short = "Search catalog resources using the query language"
		searchCmd.Long = `Search catalog resources using the catalog query language.

Use this instead of "faro catalog list" when you want field expressions in a
single query, such as type=..., tags.cluster=..., health!=healthy, name=api*,
or multiple clauses joined together.

Queries are matched against resource name, type, tags, labels, health, status,
and other indexed catalog fields. Multiple clauses are space-separated and ANDed
together. Quote the query when it contains spaces or shell-special characters.`
		searchCmd.Example = `  faro catalog search 'type=Kubernetes::Pod'
  faro catalog search 'tags.cluster=prod type=Kubernetes::Pod api'
  faro catalog search 'health!=healthy agent=all' --limit 50
  faro catalog search 'name=api*' --agent all --limit 50`
	}

	if changesCmd := catalogSubcommand(catalogCmd, "change"); changesCmd != nil {
		documentCatalogChangeCommand(changesCmd)
	}

	if insightsCmd := catalogSubcommand(catalogCmd, "insights"); insightsCmd != nil {
		documentCatalogInsightsCommand(insightsCmd)
	}
}

func disableCatalogRootList(catalogCmd *cobra.Command) {
	catalogCmd.Run = nil
	catalogCmd.RunE = nil
	catalogCmd.Args = nil
	catalogCmd.ValidArgsFunction = nil
	catalogCmd.ResetFlags()
}

func documentCatalogChangeCommand(changesCmd *cobra.Command) {
	changesCmd.Short = "Search and inspect catalog change events"
	changesCmd.Long = `Search and inspect historical change events for catalog resources.

Use "search" to find change IDs, then "get" to fetch the full change record.`
	changesCmd.Example = `  faro catalog change search 'change_type=diff'
  faro catalog change search 'type=Kubernetes::Deployment source=kubernetes' --limit 50
  faro catalog change get <change-id>`

	if searchCmd := catalogSubcommand(changesCmd, "search"); searchCmd != nil {
		searchCmd.Short = "Search catalog changes using the query language"
		searchCmd.Long = `Search catalog change events using the catalog query language.

Use this to find historical changes by change type, catalog resource type,
source, severity, name, tags, or other indexed fields. The result rows include
change IDs that can be passed to "faro catalog change get".`
		searchCmd.Example = `  faro catalog change search 'change_type=diff'
  faro catalog change search 'change_type=diff type=Kubernetes::Deployment'
  faro catalog change search 'severity=critical source=kubernetes' --limit 50`
	}

	if getCmd := catalogSubcommand(changesCmd, "get"); getCmd != nil {
		getCmd.Short = "Get full details for a catalog change"
		getCmd.Long = `Get the full details for a single catalog change event by ID.

Use "faro catalog change search" to find change IDs first.`
		getCmd.Example = `  faro catalog change get <change-id>
  faro catalog change search 'change_type=diff' --limit 10`
	}
}

func documentCatalogInsightsCommand(insightsCmd *cobra.Command) {
	insightsCmd.Short = "Search and inspect catalog insights"
	insightsCmd.Long = `Search and inspect catalog insights produced by analyzers.

Insights are findings or observations about catalog resources, such as security,
compliance, reliability, or operational issues. Use "search" to find insight
IDs, then "get" to fetch the full insight record.`
	insightsCmd.Example = `  faro catalog insights search 'severity=critical'
  faro catalog insights search 'status=open analyzer=no-public-ip' --limit 50
  faro catalog insights get <insight-id>`

	if searchCmd := catalogSubcommand(insightsCmd, "search"); searchCmd != nil {
		searchCmd.Short = "Search catalog insights using the query language"
		searchCmd.Long = `Search catalog insights using the catalog query language.

Use this to find findings by severity, status, analyzer, source, config_id,
catalog resource type, name, tags, or other indexed fields. The result rows
include insight IDs that can be passed to "faro catalog insights get".`
		searchCmd.Example = `  faro catalog insights search 'severity=critical'
  faro catalog insights search 'status=open type=security'
  faro catalog insights search 'analyzer=no-public-ip source=aws' --limit 50
  faro catalog insights search 'config_type=GitHub::Repository severity=critical' --limit 5
  faro catalog insights search 'config_id=203c4012-d12b-5c6a-a1e7-2e990f6a8f0e'`
	}

	if getCmd := catalogSubcommand(insightsCmd, "get"); getCmd != nil {
		getCmd.Short = "Get full details for a catalog insight"
		getCmd.Long = `Get the full details for a single catalog insight by ID.

Use "faro catalog insights search" to find insight IDs first.`
		getCmd.Example = `  faro catalog insights get <insight-id>
  faro catalog insights search 'severity=critical' --limit 10`
	}
}

func catalogSubcommand(parent *cobra.Command, name string) *cobra.Command {
	if parent == nil {
		return nil
	}
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}
