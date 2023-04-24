package rbac

import (
	"net/http"

	"github.com/flanksource/commons/logger"
)

var dbResourceObjMap = map[string]string{
	"responders":                      ObjectDatabaseResponder,
	"incidents":                       ObjectDatabaseIncident,
	"evidences":                       ObjectDatabaseEvidence,
	"comments":                        ObjectDatabaseComment,
	"canaries":                        ObjectDatabaseCanary,
	"system_templates":                ObjectDatabaseSystemTemplate,
	"config_scrapers":                 ObjectDatabaseConfigScraper,
	"connections":                     ObjectDatabaseConnection,
	"identities":                      ObjectDatabaseIdentity,
	"identity_credential_identifiers": ObjectDatabaseKratosTable,
	"identity_credential_types":       ObjectDatabaseKratosTable,
	"identity_credentials":            ObjectDatabaseKratosTable,
	"identity_recovery_addresses":     ObjectDatabaseKratosTable,
	"identity_recovery_tokens":        ObjectDatabaseKratosTable,
	"identity_verifiable_addresses":   ObjectDatabaseKratosTable,
	"identity_verification_tokens":    ObjectDatabaseKratosTable,
	"schema_migration":                ObjectDatabaseKratosTable,
	"selfservice_errors":              ObjectDatabaseKratosTable,
	"selfservice_login_flows":         ObjectDatabaseKratosTable,
	"selfservice_recovery_flows":      ObjectDatabaseKratosTable,
	"selfservice_settings_flows":      ObjectDatabaseKratosTable,
	"selfservice_verification_flows":  ObjectDatabaseKratosTable,
	"courier_messages":                ObjectDatabaseKratosTable,
}

var dbReadDenied = []string{
	ObjectDatabaseSystemTemplate,
	ObjectDatabaseCanary,
	ObjectDatabaseConfigScraper,
	ObjectDatabaseConnection,
	ObjectDatabaseIdentity,
	ObjectDatabaseKratosTable,
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
		return ActionUpdate
	case http.MethodPost:
		return ActionCreate
	}
	return ""
}
