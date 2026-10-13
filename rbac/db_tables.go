package rbac

import (
	"path"
	"strings"

	"github.com/flanksource/duty/rbac/policy"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// dbTableKind is why /db serves a table (specs/authorization/roles.md, Section 3.1).
type dbTableKind int

const (
	// dbType is a type's table, or a view of its rows (specs/authorization/scopes.md, Section 3.1).
	dbType dbTableKind = iota + 1
	// dbRecord is data that belongs to resources of the types (specs/authorization/scopes.md, Section 3.2).
	dbRecord
	// dbLookup is a lookup view derived from the type, e.g. the distinct values or counts the UI's filters offer.
	dbLookup
	// dbAction is read whole or not at all through an action without a resource: job:read, event:read,
	// or properties (specs/authorization/roles.md, Section 2.3).
	dbAction
	// dbUnclassified fits none of the other kinds, and is only served because the UI calls it.
	// TODO: each is an open question for the spec, which says /db doesn't serve such a table.
	dbUnclassified
)

// dbTable is a table /db serves, and why.
type dbTable struct {
	kind dbTableKind
	// of is the types the table is, belongs to or is derived from. For dbAction, it's the object the action is
	// granted on, and for dbUnclassified, what the table is.
	of []string
}

func typeTable(resourceType string) dbTable { return dbTable{dbType, []string{resourceType}} }
func recordOf(types ...string) dbTable      { return dbTable{dbRecord, types} }
func lookupOf(resourceType string) dbTable  { return dbTable{dbLookup, []string{resourceType}} }
func readWithAction(object string) dbTable  { return dbTable{dbAction, []string{object}} }
func unclassified(what string) dbTable      { return dbTable{dbUnclassified, []string{what}} }

const (
	ofUnclassifiedIncidents          = "incidents, which aren't a type"
	ofUnclassifiedLegacyPermissions  = "legacy Permissions, which aren't a type"
	ofUnclassifiedExternalIdentities = "users, groups and roles a scraper found in another system"
	ofUnclassifiedIntegrations       = "topologies and logging backends, which aren't types, alongside scrapers"
	ofUnclassifiedSavedQueries       = "a person's saved searches"
	ofUnclassifiedPluginDefinitions  = "Plugin and ScrapePlugin definitions, which aren't types"
)

// dbTables are the tables and functions /db serves. A table that isn't one of these isn't served,
// even to admin, whatever object duty's /db map gives it.
var dbTables = map[string]dbTable{
	// Types
	"agents":                           typeTable(v1.ResourceAgent),
	"agents_summary":                   typeTable(v1.ResourceAgent),
	"applications":                     typeTable(v1.ResourceApplication),
	"canaries":                         typeTable(policy.ResourceCanary),
	"canaries_with_status":             typeTable(policy.ResourceCanary),
	"rpc/soft_delete_canary":           typeTable(policy.ResourceCanary),
	"checks":                           typeTable(policy.ResourceCheck),
	"checks_unlogged":                  typeTable(policy.ResourceCheck),
	"check_summary":                    typeTable(policy.ResourceCheck),
	"rpc/check_summary_for_component":  typeTable(policy.ResourceCheck),
	"rpc/soft_delete_check":            typeTable(policy.ResourceCheck),
	"components":                       typeTable(policy.ResourceComponent),
	"components_with_logs":             typeTable(policy.ResourceComponent),
	"topology":                         typeTable(policy.ResourceComponent),
	"rpc/lookup_component_by_property": typeTable(policy.ResourceComponent),
	"rpc/lookup_component_children":    typeTable(policy.ResourceComponent),
	"rpc/lookup_component_config_id_related_components": typeTable(policy.ResourceComponent),
	"rpc/lookup_component_relations":                    typeTable(policy.ResourceComponent),
	"rpc/lookup_components_by_check":                    typeTable(policy.ResourceComponent),
	"rpc/lookup_components_by_config":                   typeTable(policy.ResourceComponent),
	"config_items":                                      typeTable(policy.ResourceConfig),
	"configs":                                           typeTable(policy.ResourceConfig),
	"config_detail":                                     typeTable(policy.ResourceConfig),
	"config_items_aws":                                  typeTable(policy.ResourceConfig),
	"rpc/_related_config_ids_recursive":                 typeTable(policy.ResourceConfig),
	"rpc/get_children_by_location":                      typeTable(policy.ResourceConfig),
	"rpc/get_children_id_by_location":                   typeTable(policy.ResourceConfig),
	"rpc/get_parent_ids_by_location":                    typeTable(policy.ResourceConfig),
	"rpc/get_parents_by_location":                       typeTable(policy.ResourceConfig),
	"rpc/get_recursive_path":                            typeTable(policy.ResourceConfig),
	"rpc/lookup_config_children":                        typeTable(policy.ResourceConfig),
	"rpc/lookup_config_relations":                       typeTable(policy.ResourceConfig),
	"rpc/lookup_configs_by_component":                   typeTable(policy.ResourceConfig),
	"rpc/lookup_related_configs":                        typeTable(policy.ResourceConfig),
	"rpc/related_config_ids":                            typeTable(policy.ResourceConfig),
	"rpc/related_config_ids_recursive":                  typeTable(policy.ResourceConfig),
	"rpc/related_configs":                               typeTable(policy.ResourceConfig),
	"rpc/related_configs_recursive":                     typeTable(policy.ResourceConfig),
	"config_scrapers":                                   typeTable(policy.ResourceScraper),
	"config_scrapers_with_status":                       typeTable(policy.ResourceScraper),
	"connections":                                       typeTable(policy.ResourceConnection),
	"connections_list":                                  typeTable(policy.ResourceConnection),
	"connection_details":                                typeTable(policy.ResourceConnection),
	"notifications":                                     typeTable(v1.ResourceNotification),
	"notifications_summary":                             typeTable(v1.ResourceNotification),
	"notification_silences":                             typeTable(v1.ResourceNotificationSilence),
	"people":                                            typeTable(v1.ResourcePerson),
	"playbooks":                                         typeTable(policy.ResourcePlaybook),
	"teams":                                             typeTable(v1.ResourceTeam),
	"teams_with_status":                                 typeTable(v1.ResourceTeam),
	"views":                                             typeTable(policy.ResourceView),
	"views_summary":                                     typeTable(policy.ResourceView),
	"scopes":                                            typeTable(v1.ResourceScope),
	"roles":                                             typeTable(v1.ResourceRole),
	"role_bindings":                                     typeTable(v1.ResourceRoleBinding),

	// Records
	"access_reviews":                            recordOf(policy.ResourceConfig),
	"analysis_by_component":                     recordOf(policy.ResourceConfig, policy.ResourceComponent),
	"analysis_by_config":                        recordOf(policy.ResourceConfig),
	"analysis_summary_by_component":             recordOf(policy.ResourceConfig, policy.ResourceComponent),
	"catalog_changes":                           recordOf(policy.ResourceConfig),
	"changes_by_component":                      recordOf(policy.ResourceConfig, policy.ResourceComponent),
	"config_access":                             recordOf(policy.ResourceConfig),
	"config_access_logs":                        recordOf(policy.ResourceConfig),
	"config_access_summary":                     recordOf(policy.ResourceConfig),
	"config_access_summary_by_config":           recordOf(policy.ResourceConfig),
	"config_access_summary_by_group":            recordOf(policy.ResourceConfig),
	"config_access_summary_by_user":             recordOf(policy.ResourceConfig),
	"config_access_unwrapped":                   recordOf(policy.ResourceConfig),
	"config_analysis":                           recordOf(policy.ResourceConfig),
	"config_analysis_items":                     recordOf(policy.ResourceConfig),
	"config_changes":                            recordOf(policy.ResourceConfig),
	"config_changes_items":                      recordOf(policy.ResourceConfig),
	"config_component_relationships":            recordOf(policy.ResourceConfig, policy.ResourceComponent),
	"config_cost_compact":                       recordOf(policy.ResourceConfig),
	"config_costs":                              recordOf(policy.ResourceConfig),
	"config_items_last_scraped_time":            recordOf(policy.ResourceConfig),
	"config_locations":                          recordOf(policy.ResourceConfig),
	"config_relationships":                      recordOf(policy.ResourceConfig),
	"rpc/config_relationships_recursive":        recordOf(policy.ResourceConfig),
	"rpc/lookup_analysis_by_component":          recordOf(policy.ResourceConfig, policy.ResourceComponent),
	"rpc/lookup_changes_by_component":           recordOf(policy.ResourceConfig, policy.ResourceComponent),
	"rpc/related_changes_recursive":             recordOf(policy.ResourceConfig),
	"check_component_relationships":             recordOf(policy.ResourceCheck, policy.ResourceComponent),
	"check_config_relationships":                recordOf(policy.ResourceCheck, policy.ResourceConfig),
	"check_status_summary_hour":                 recordOf(policy.ResourceCheck),
	"check_statuses":                            recordOf(policy.ResourceCheck),
	"check_statuses_1d":                         recordOf(policy.ResourceCheck),
	"check_statuses_1h":                         recordOf(policy.ResourceCheck),
	"check_statuses_5m":                         recordOf(policy.ResourceCheck),
	"checks_by_component":                       recordOf(policy.ResourceCheck, policy.ResourceComponent),
	"check_summary_for_config":                  recordOf(policy.ResourceCheck, policy.ResourceConfig),
	"checks_by_config":                          recordOf(policy.ResourceCheck, policy.ResourceConfig),
	"checks_status_artifacts":                   recordOf(policy.ResourceCheck),
	"component_relationships":                   recordOf(policy.ResourceComponent),
	"playbook_action_agent_data":                recordOf(policy.ResourcePlaybook, policy.ResourceConfig, policy.ResourceCheck),
	"playbook_approvals":                        recordOf(policy.ResourcePlaybook, policy.ResourceConfig, policy.ResourceCheck),
	"playbook_run_actions":                      recordOf(policy.ResourcePlaybook, policy.ResourceConfig, policy.ResourceCheck),
	"playbook_runs":                             recordOf(policy.ResourcePlaybook, policy.ResourceConfig, policy.ResourceCheck),
	"rpc/get_playbook_run_actions":              recordOf(policy.ResourcePlaybook, policy.ResourceConfig, policy.ResourceCheck),
	"notification_group_resources":              recordOf(v1.ResourceNotification),
	"notification_groups":                       recordOf(v1.ResourceNotification),
	"notification_send_history":                 recordOf(v1.ResourceNotification),
	"notification_send_history_resources":       recordOf(v1.ResourceNotification),
	"rpc/notification_send_history_of_resource": recordOf(v1.ResourceNotification),
	"artifacts":                                 recordOf(policy.ResourcePlaybook, policy.ResourceCheck, policy.ResourceConfig, policy.ResourceScraper),
	"artifact_summary":                          recordOf(policy.ResourcePlaybook, policy.ResourceCheck, policy.ResourceConfig, policy.ResourceScraper),
	"identities":                                recordOf(v1.ResourcePerson),
	"people_roles":                              recordOf(v1.ResourcePerson),
	"person_analytics":                          recordOf(v1.ResourcePerson),
	"team_components":                           recordOf(v1.ResourceTeam, policy.ResourceComponent),
	"team_members":                              recordOf(v1.ResourceTeam),
	"scope_targets":                             recordOf(v1.ResourceScope),
	"view_panels":                               recordOf(policy.ResourceView),

	// Lookup views
	"analysis_types":                           lookupOf(policy.ResourceConfig),
	"analyzer_types":                           lookupOf(policy.ResourceConfig),
	"change_types":                             lookupOf(policy.ResourceConfig),
	"config_analysis_analyzers":                lookupOf(policy.ResourceConfig),
	"config_analysis_by_severity":              lookupOf(policy.ResourceConfig),
	"config_changes_by_types":                  lookupOf(policy.ResourceConfig),
	"config_class_summary":                     lookupOf(policy.ResourceConfig),
	"config_classes":                           lookupOf(policy.ResourceConfig),
	"config_labels":                            lookupOf(policy.ResourceConfig),
	"config_names":                             lookupOf(policy.ResourceConfig),
	"config_statuses":                          lookupOf(policy.ResourceConfig),
	"config_summary":                           lookupOf(policy.ResourceConfig),
	"config_tags":                              lookupOf(policy.ResourceConfig),
	"config_tags_labels_keys":                  lookupOf(policy.ResourceConfig),
	"config_types":                             lookupOf(policy.ResourceConfig),
	"rpc/config_access_filter_options":         lookupOf(policy.ResourceConfig),
	"rpc/config_analysis_filter_options":       lookupOf(policy.ResourceConfig),
	"check_labels":                             lookupOf(policy.ResourceCheck),
	"check_names":                              lookupOf(policy.ResourceCheck),
	"check_summary_by_component":               lookupOf(policy.ResourceCheck),
	"check_summary_by_config":                  lookupOf(policy.ResourceCheck),
	"checks_labels_keys":                       lookupOf(policy.ResourceCheck),
	"component_labels":                         lookupOf(policy.ResourceComponent),
	"component_labels_keys":                    lookupOf(policy.ResourceComponent),
	"component_names":                          lookupOf(policy.ResourceComponent),
	"component_names_all":                      lookupOf(policy.ResourceComponent),
	"component_types":                          lookupOf(policy.ResourceComponent),
	"rpc/lookup_component_names":               lookupOf(policy.ResourceComponent),
	"notification_send_history_resource_tags":  lookupOf(v1.ResourceNotification),
	"notification_send_history_resource_types": lookupOf(v1.ResourceNotification),
	"notification_send_history_summary":        lookupOf(v1.ResourceNotification),
	"playbook_names":                           lookupOf(policy.ResourcePlaybook),
	"playbooks_for_agent":                      lookupOf(policy.ResourcePlaybook),

	// Read whole through an action without a resource
	"job_histories":             readWithAction(policy.ObjectJobs),
	"job_history":               readWithAction(policy.ObjectJobs),
	"job_history_latest_status": readWithAction(policy.ObjectJobs),
	"job_history_names":         readWithAction(policy.ObjectJobs),
	"job_history_summary":       readWithAction(policy.ObjectJobs),
	"event_queue":               readWithAction(policy.ObjectEvents),
	"event_queue_summary":       readWithAction(policy.ObjectEvents),
	"failed_events":             readWithAction(policy.ObjectEvents),
	"properties":                readWithAction(policy.ObjectProperties),

	// Unclassified, served because the UI calls them
	"incidents":                        unclassified(ofUnclassifiedIncidents),
	"incident_histories":               unclassified(ofUnclassifiedIncidents),
	"incident_rules":                   unclassified(ofUnclassifiedIncidents),
	"incident_summary":                 unclassified(ofUnclassifiedIncidents),
	"incidents_by_component":           unclassified(ofUnclassifiedIncidents),
	"incidents_by_config":              unclassified(ofUnclassifiedIncidents),
	"comments":                         unclassified(ofUnclassifiedIncidents),
	"evidences":                        unclassified(ofUnclassifiedIncidents),
	"hypotheses":                       unclassified(ofUnclassifiedIncidents),
	"responders":                       unclassified(ofUnclassifiedIncidents),
	"permissions":                      unclassified(ofUnclassifiedLegacyPermissions),
	"permissions_summary":              unclassified(ofUnclassifiedLegacyPermissions),
	"permission_subjects":              unclassified(ofUnclassifiedLegacyPermissions),
	"rpc/permissions_for_obj_selector": unclassified(ofUnclassifiedLegacyPermissions),
	"external_users":                   unclassified(ofUnclassifiedExternalIdentities),
	"external_groups":                  unclassified(ofUnclassifiedExternalIdentities),
	"external_roles":                   unclassified(ofUnclassifiedExternalIdentities),
	"external_user_groups":             unclassified(ofUnclassifiedExternalIdentities),
	"external_group_summary":           unclassified(ofUnclassifiedExternalIdentities),
	"rpc/add_external_user_alias":      unclassified(ofUnclassifiedExternalIdentities),
	"rpc/merge_external_users":         unclassified(ofUnclassifiedExternalIdentities),
	"integrations":                     unclassified(ofUnclassifiedIntegrations),
	"integrations_with_status":         unclassified(ofUnclassifiedIntegrations),
	"logging_backends":                 unclassified(ofUnclassifiedIntegrations),
	"topologies":                       unclassified(ofUnclassifiedIntegrations),
	"topologies_with_status":           unclassified(ofUnclassifiedIntegrations),
	"saved_query":                      unclassified(ofUnclassifiedSavedQueries),
	"plugins":                          unclassified(ofUnclassifiedPluginDefinitions),
	"scrape_plugins":                   unclassified(ofUnclassifiedPluginDefinitions),
}

// servedTable returns why /db serves the table, if it does. A View's generated tables, view_<namespace>_<name>,
// are its records.
// A path with dot segments isn't served: the proxy resolves them, so view_x/../access_tokens would reach access_tokens.
func servedTable(table string) (dbTable, bool) {
	if path.Clean("/"+table) != "/"+table {
		return dbTable{}, false
	}
	if t, ok := dbTables[table]; ok {
		return t, true
	}
	if strings.HasPrefix(table, "view_") && !strings.Contains(table, "/") {
		return recordOf(policy.ResourceView), true
	}
	return dbTable{}, false
}
