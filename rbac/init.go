package rbac

import (
	"fmt"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
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

	// Actions
	ActionRead   = "read"
	ActionWrite  = "write"
	ActionUpdate = "update"
	ActionCreate = "create"

	// Objects
	ObjectRBAC     = "rbac"
	ObjectAuth     = "auth"
	ObjectDatabase = "database"

	ObjectDatabaseResponder      = "database.responder"
	ObjectDatabaseIncident       = "database.incident"
	ObjectDatabaseEvidence       = "database.evidences"
	ObjectDatabaseComment        = "database.comments"
	ObjectDatabaseCanary         = "database.canaries"
	ObjectDatabaseSystemTemplate = "database.system_templates"
	ObjectDatabaseConfigScraper  = "database.config_scrapers"
)

var Enforcer *casbin.Enforcer

func Init() error {
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

	if _, err := Enforcer.AddRoleForUser(api.SystemUserID.String(), RoleAdmin); err != nil {
		return fmt.Errorf("error adding role for system admin user: %v", err)
	}

	// TODO: Remove
	//if _, err := Enforcer.AddRoleForUser("viewer-user", RoleViewer); err != nil {
	//return fmt.Errorf("error adding role for system admin user: %v", err)
	//}

	// TODO: Add  hierarchial policies
	//if _, err := Enforcer.AddGroupingPolicy(RoleViewer, RoleAdmin); err != nil {
	//return fmt.Errorf("error adding role for system admin user: %v", err)
	//}

	policies := [][]string{
		// If the user is admin, no check takes place
		// we have these policies as placeholders
		{RoleAdmin, ObjectDatabase, ActionRead},
		{RoleAdmin, ObjectDatabase, ActionWrite},
		{RoleAdmin, ObjectRBAC, ActionWrite},
		{RoleAdmin, ObjectAuth, ActionWrite},

		{RoleEditor, ObjectDatabaseCanary, ActionCreate},
		{RoleEditor, ObjectDatabaseCanary, ActionUpdate},
		{RoleEditor, ObjectDatabaseSystemTemplate, ActionCreate},
		{RoleEditor, ObjectDatabaseSystemTemplate, ActionUpdate},
		{RoleEditor, ObjectDatabaseConfigScraper, ActionCreate},
		{RoleEditor, ObjectDatabaseConfigScraper, ActionUpdate},

		{RoleCommander, ObjectDatabaseResponder, ActionCreate},
		{RoleCommander, ObjectDatabaseIncident, ActionCreate},
		{RoleCommander, ObjectDatabaseIncident, ActionUpdate},
		{RoleCommander, ObjectDatabaseEvidence, ActionCreate},
		{RoleCommander, ObjectDatabaseEvidence, ActionUpdate},

		{RoleResponder, ObjectDatabaseComment, ActionCreate},
		{RoleResponder, ObjectDatabaseIncident, ActionUpdate},

		{RoleViewer, ObjectDatabase, ActionRead},
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
