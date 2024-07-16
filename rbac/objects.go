package rbac

import (
	"net/http"

	"github.com/flanksource/commons/logger"
)

var dbResourceObjMap = map[string]string{
	"access_token":                     ObjectAuthConfidential,
	"agents_summary":                   ObjectMonitor,
	"agents":                           ObjectAgent,
	"analysis_by_component":            ObjectCatalog,
	"analysis_by_config":               ObjectCatalog,
	"analysis_summary_by_component":    ObjectCatalog,
	"analysis_types":                   ObjectDatabasePublic,
	"analyzer_types":                   ObjectDatabasePublic,
	"artifacts":                        ObjectArtifact,
	"canaries_with_status":             ObjectCanary,
	"canaries":                         ObjectCanary,
	"casbin_rule":                      ObjectAuth,
	"change_types":                     ObjectDatabasePublic,
	"changes_by_component":             ObjectCatalog,
	"check_component_relationships":    ObjectCanary,
	"check_config_relationships":       ObjectCanary,
	"check_labels":                     ObjectDatabasePublic,
	"check_names":                      ObjectDatabasePublic,
	"check_status_summary_hour":        ObjectCanary,
	"check_statuses_1d":                ObjectCanary,
	"check_statuses_1h":                ObjectCanary,
	"check_statuses_5m":                ObjectCanary,
	"check_statuses":                   ObjectCanary,
	"check_summary_by_component":       ObjectCanary,
	"check_summary":                    ObjectCanary,
	"checks_by_component":              ObjectCanary,
	"checks":                           ObjectCanary,
	"comment_responders":               ObjectIncident,
	"comments":                         ObjectIncident,
	"component_labels":                 ObjectDatabasePublic,
	"component_names_all":              ObjectTopology,
	"component_names":                  ObjectTopology,
	"component_relationships":          ObjectTopology,
	"component_types":                  ObjectDatabasePublic,
	"components_with_logs":             ObjectTopology,
	"components":                       ObjectTopology,
	"config_analysis_analyzers":        ObjectCatalog,
	"config_analysis_by_severity":      ObjectCatalog,
	"config_analysis":                  ObjectCatalog,
	"config_changes_by_types":          ObjectCatalog,
	"config_changes":                   ObjectCatalog,
	"config_class_summary":             ObjectCatalog,
	"config_classes":                   ObjectDatabasePublic,
	"config_component_relationships":   ObjectCatalog,
	"config_detail":                    ObjectCatalog,
	"config_items_aws":                 ObjectCatalog,
	"config_items":                     ObjectCatalog,
	"config_names":                     ObjectCatalog,
	"config_relationships":             ObjectCatalog,
	"config_scrapers_with_status":      ObjectMonitor,
	"config_scrapers":                  ObjectDatabaseSettings,
	"config_summary":                   ObjectCatalog,
	"config_tags":                      ObjectCatalog,
	"config_types":                     ObjectDatabasePublic,
	"configs":                          ObjectCatalog,
	"connections":                      ObjectDatabaseSystem,
	"event_queue":                      ObjectMonitor,
	"evidences":                        ObjectIncident,
	"hypotheses":                       ObjectIncident,
	"identities":                       ObjectDatabasePublic,
	"courier_messages":                 ObjectAuthConfidential,
	"courier_messaged_dispatches":      ObjectAuthConfidential,
	"identity_credential_identifiers":  ObjectAuthConfidential,
	"identity_credential_types":        ObjectAuthConfidential,
	"identity_credentials":             ObjectAuthConfidential,
	"identity_recovery_addresses":      ObjectAuthConfidential,
	"identity_recovery_tokens":         ObjectAuthConfidential,
	"identity_verifiable_addresses":    ObjectAuthConfidential,
	"identity_verification_tokens":     ObjectAuthConfidential,
	"sessions":                         ObjectAuthConfidential,
	"session_devices":                  ObjectAuthConfidential,
	"schema_migration":                 ObjectAuthConfidential,
	"selfservice_errors":               ObjectAuthConfidential,
	"selfservice_login_flows":          ObjectAuthConfidential,
	"selfservice_recovery_flows":       ObjectAuthConfidential,
	"selfservice_settings_flows":       ObjectAuthConfidential,
	"selfservice_verification_flows":   ObjectAuthConfidential,
	"networks":                         ObjectAuthConfidential,
	"incident_histories":               ObjectIncident,
	"incident_relationships":           ObjectIncident,
	"incident_rules":                   ObjectIncident,
	"incident_summary_by_component":    ObjectIncident,
	"incident_summary":                 ObjectIncident,
	"incidents_by_component":           ObjectIncident,
	"incidents_by_config":              ObjectIncident,
	"incidents":                        ObjectIncident,
	"integrations_with_status":         ObjectMonitor,
	"integrations":                     ObjectMonitor,
	"job_history_latest_status":        ObjectMonitor,
	"job_history_names":                ObjectMonitor,
	"job_history":                      ObjectMonitor,
	"logging_backends":                 ObjectDatabaseSettings,
	"migration_logs":                   ObjectDatabaseSystem,
	"notification_send_history":        ObjectMonitor,
	"notifications_summary":            ObjectMonitor,
	"notifications":                    ObjectDatabaseSettings,
	"people_roles":                     ObjectDatabasePublic,
	"people":                           ObjectDatabasePublic,
	"playbook_action_agent_data":       ObjectPlaybooks,
	"playbook_approvals":               ObjectPlaybooks,
	"playbook_run_actions":             ObjectPlaybooks,
	"playbook_runs":                    ObjectPlaybooks,
	"playbooks_for_agent":              ObjectAgentPush,
	"playbooks":                        ObjectPlaybooks,
	"properties":                       ObjectDatabaseSystem,
	"responders":                       ObjectIncident,
	"rpc/check_summary_for_component":  ObjectCanary,
	"rpc/lookup_analysis_by_component": ObjectTopology,
	"rpc/lookup_changes_by_component":  ObjectTopology,
	"rpc/lookup_component_by_property": ObjectTopology,
	"rpc/lookup_component_children":    ObjectTopology,
	"rpc/lookup_component_incidents":   ObjectTopology,
	"rpc/lookup_component_names":       ObjectTopology,
	"rpc/lookup_component_relations":   ObjectTopology,
	"rpc/lookup_components_by_check":   ObjectTopology,
	"rpc/lookup_components_by_config":  ObjectTopology,
	"rpc/lookup_config_children":       ObjectCatalog,
	"rpc/lookup_config_relations":      ObjectCatalog,
	"rpc/lookup_configs_by_component":  ObjectTopology,
	"rpc/lookup_related_configs":       ObjectCatalog,
	"rpc/soft_delete_canary":           ObjectCanary,
	"rpc/soft_delete_check":            ObjectCanary,
	"rpc/uuid_to_ulid":                 ObjectDatabasePublic,
	"saved_query":                      ObjectDatabasePublic,
	"severities":                       ObjectDatabasePublic,
	"system_templates":                 ObjectTopology,
	"team_components":                  ObjectDatabasePublic,
	"team_members":                     ObjectDatabasePublic,
	"teams_with_status":                ObjectDatabasePublic,
	"teams":                            ObjectDatabasePublic,
	"topologies_with_status":           ObjectTopology,
	"topologies":                       ObjectTopology,
	"topology":                         ObjectTopology,
}

func postgrestDatabaseObject(resource string) string {
	if v, exists := dbResourceObjMap[resource]; exists {
		return v
	}

	logger.Errorf("Got unknown table for rbac: %s", resource)
	return ""
}

func policyActionFromHTTPMethod(method string) string {
	switch method {
	case http.MethodGet:
		return ActionRead
	case http.MethodPatch:
		return ActionWrite
	case http.MethodPost:
		return ActionCreate
	case http.MethodDelete:
		return ActionDelete
	}

	return ""
}
