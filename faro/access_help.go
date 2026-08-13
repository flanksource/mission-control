package main

import "github.com/spf13/cobra"

func documentAccessCommand(accessCmd *cobra.Command) {
	if accessCmd == nil {
		return
	}

	disableAccessRootList(accessCmd)

	accessCmd.Short = "Export access and RBAC data for auditing"
	accessCmd.Long = `Export the access data discovered from Azure AD, AWS, Kubernetes and other
connected systems — the same data that backs the access matrix and access
sections of the catalog report.

Use "permissions" for the config × principal crosstab, "users"/"groups"/"roles"
to inspect a single principal, and "logs"/"reviews" for the time-ordered
history. Named filters use Commons MatchItem syntax: exact matches by default,
"*" wildcards, comma-separated alternatives, and "!" exclusions. Quote filter
values containing wildcards. Every subcommand honours --csv, --json, --yaml and
--markdown.`
	accessCmd.Example = `  faro access permissions --limit 20
  faro access permissions --config-type 'Azure::*,!Azure::Legacy*' --csv
  faro access permissions --user '*@example.com,!svc-*'
  faro access users get jane@example.com
  faro access groups list --type 'Security*,!SecurityLegacy'
  faro access logs --since 30d`

	if usersCmd := subcommand(accessCmd, "users"); usersCmd != nil {
		usersCmd.Short = "List and inspect external users"
		usersCmd.Long = `List and inspect the external users discovered by access scrapers.

"list" filters by name, email, alias or user type. "get" resolves a single user
by id, name, email or alias and additionally shows the configs they can reach
and the groups they belong to.`
		usersCmd.Example = `  faro access users list --name '*@example.com,!svc-*'
  faro access users list --type 'Human,Service*'
  faro access users get jane@example.com
  faro access users get <user-id> --groups=false`
	}

	if groupsCmd := subcommand(accessCmd, "groups"); groupsCmd != nil {
		groupsCmd.Short = "List and inspect external groups"
		groupsCmd.Long = `List and inspect the external groups discovered by access scrapers.

"list" returns each group with its member and permission counts. "get" resolves
a single group and shows its membership — including revoked memberships, marked
as removed — plus the configs the group grants access to.`
		groupsCmd.Example = `  faro access groups list --name 'sre*,!sre-retired'
  faro access groups list --type 'Security*,!SecurityLegacy' --csv
  faro access groups get sre-team
  faro access groups get <group-id> --access=false`
	}

	if rolesCmd := subcommand(accessCmd, "roles"); rolesCmd != nil {
		rolesCmd.Short = "List and inspect external roles"
		rolesCmd.Long = `List and inspect the external roles discovered by access scrapers.

"get" resolves a single role and shows the users and groups currently holding
it through live grants.`
		rolesCmd.Example = `  faro access roles list --name 'Owner,Reader*'
  faro access roles list --type 'BuiltIn*,!BuiltInLegacy'
  faro access roles get Owner`
	}
}

func disableAccessRootList(accessCmd *cobra.Command) {
	accessCmd.Run = nil
	accessCmd.RunE = nil
	accessCmd.Args = nil
	accessCmd.ValidArgsFunction = nil
	accessCmd.ResetFlags()
}
