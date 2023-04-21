package rbac

import (
	"net/http"

	"github.com/flanksource/commons/logger"
)

var dbResourceObjMap = map[string]string{
	"responders":       ObjectDatabaseResponder,
	"incidents":        ObjectDatabaseIncident,
	"evidences":        ObjectDatabaseEvidence,
	"comments":         ObjectDatabaseComment,
	"canaries":         ObjectDatabaseCanary,
	"system_templates": ObjectDatabaseSystemTemplate,
	"config_scrapers":  ObjectDatabaseConfigScraper,
	"identities":       ObjectDatabaseIdentity,
	"connections":      ObjectDatabaseConnection,
}

var dbReadDenied = []string{
	ObjectDatabaseSystemTemplate,
	ObjectDatabaseCanary,
	ObjectDatabaseConfigScraper,
	ObjectDatabaseConnection,
	ObjectDatabaseIdentity,
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
