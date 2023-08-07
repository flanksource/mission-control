package rbac

import (
	"fmt"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
)

const (
	modelDefinition = `
    [request_definition]
    r = sub, obj, act

    [policy_definition]
    p = sub, obj, act

    [role_definition]
    g = _, _

    [policy_effect]
    e = some(where (p.eft == allow))

    [matchers]
    m = g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act`

	// Roles
	RoleAdmin     = "admin"
	RoleEditor    = "editor"
	RoleViewer    = "viewer"
	RoleCommander = "commander"
	RoleResponder = "responder"
	RoleAgent     = "agent"

	// Actions
	ActionRead   = "read"
	ActionWrite  = "write"
	ActionUpdate = "update"
	ActionCreate = "create"

	// Objects
	ObjectRBAC      = "rbac"
	ObjectAuth      = "auth"
	ObjectAgentPush = "agent-push"
	ObjectDatabase  = "database"

	ObjectDatabaseResponder      = "database.responder"
	ObjectDatabaseIncident       = "database.incident"
	ObjectDatabaseEvidence       = "database.evidences"
	ObjectDatabaseComment        = "database.comments"
	ObjectDatabaseCanary         = "database.canaries"
	ObjectDatabaseSystemTemplate = "database.system_templates"
	ObjectDatabaseConfigScraper  = "database.config_scrapers"
	ObjectDatabaseIdentity       = "database.identities"
	ObjectDatabaseConnection     = "database.connections"
	ObjectDatabaseKratosTable    = "database.kratos"
)

var Enforcer *casbin.Enforcer

func Init(adminUserID string) error {
	model, err := model.NewModelFromString(modelDefinition)
	if err != nil {
		return fmt.Errorf("error creating rbac model: %v", err)
	}

	gormadapter.TurnOffAutoMigrate(db.Gorm)
	adapter, err := gormadapter.NewAdapterByDB(db.Gorm)
	if err != nil {
		return fmt.Errorf("error creating rbac adapter: %v", err)
	}

	Enforcer, err = casbin.NewEnforcer(model, adapter)
	if err != nil {
		return fmt.Errorf("error creating rbac enforcer: %v", err)
	}

	if adminUserID != "" {
		if _, err := Enforcer.AddRoleForUser(adminUserID, RoleAdmin); err != nil {
			return fmt.Errorf("error adding role for admin user: %v", err)
		}
	}

	// Hierarchial policies
	if _, err := Enforcer.AddGroupingPolicy(RoleEditor, RoleCommander); err != nil {
		return fmt.Errorf("error adding group policy for role editor to commander: %v", err)
	}
	if _, err := Enforcer.AddGroupingPolicy(RoleCommander, RoleResponder); err != nil {
		return fmt.Errorf("error adding group policy for role commander to responder: %v", err)
	}

	policies := [][]string{
		// If the user is admin, no check takes place
		// we have these policies as placeholders
		{RoleAdmin, ObjectDatabase, ActionRead},
		{RoleAdmin, ObjectDatabase, ActionWrite},
		{RoleAdmin, ObjectRBAC, ActionWrite},
		{RoleAdmin, ObjectAuth, ActionWrite},
		{RoleAdmin, ObjectDatabaseIdentity, ActionRead},
		{RoleAdmin, ObjectDatabaseConnection, ActionRead},
		{RoleAdmin, ObjectDatabaseConnection, ActionCreate},
		{RoleAdmin, ObjectDatabaseConnection, ActionUpdate},

		{RoleEditor, ObjectDatabaseCanary, ActionCreate},
		{RoleEditor, ObjectDatabaseCanary, ActionUpdate},
		{RoleEditor, ObjectDatabaseCanary, ActionRead},
		{RoleEditor, ObjectDatabaseSystemTemplate, ActionCreate},
		{RoleEditor, ObjectDatabaseSystemTemplate, ActionUpdate},
		{RoleEditor, ObjectDatabaseSystemTemplate, ActionRead},
		{RoleEditor, ObjectDatabaseConfigScraper, ActionCreate},
		{RoleEditor, ObjectDatabaseConfigScraper, ActionUpdate},
		{RoleEditor, ObjectDatabaseConfigScraper, ActionRead},

		{RoleCommander, ObjectDatabaseResponder, ActionCreate},
		{RoleCommander, ObjectDatabaseIncident, ActionCreate},
		{RoleCommander, ObjectDatabaseIncident, ActionUpdate},
		{RoleCommander, ObjectDatabaseEvidence, ActionCreate},
		{RoleCommander, ObjectDatabaseEvidence, ActionUpdate},

		{RoleResponder, ObjectDatabaseComment, ActionCreate},
		{RoleResponder, ObjectDatabaseIncident, ActionUpdate},

		{RoleViewer, ObjectDatabase, ActionRead},

		{RoleAgent, ObjectAgentPush, ActionWrite},
	}

	// Adding policies in a loop is important
	// If we use Enforcer.AddPolicies(), new policies do not get saved
	for _, p := range policies {
		if _, err := Enforcer.AddPolicy(p); err != nil {
			return fmt.Errorf("error adding rbac policy %s: %v", p, err)
		}
	}

	// Update policies every 5 minutes
	go func() {
		time.Sleep(5 * time.Minute)
		if err := Enforcer.LoadPolicy(); err != nil {
			logger.Errorf("Error loading rbac policies: %v", err)
		}
	}()

	return nil
}

func Check(subject, object, action string) bool {
	allowed, err := Enforcer.Enforce(subject, object, action)
	if err != nil {
		logger.Errorf("RBAC Enforce failed: %v", err)
	}
	return allowed
}
