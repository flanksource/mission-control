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
	RoleDeveloper = "developer"
	RoleViewer    = "viewer"

	// Actions
	ActionRead  = "read"
	ActionWrite = "write"

	// Objects
	ObjectDatabase = "database"
	ObjectRBAC     = "rbac"
	ObjectAuth     = "auth"
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

	if _, err := Enforcer.AddRoleForUser("viewer-user", RoleViewer); err != nil {
		return fmt.Errorf("error adding role for system admin user: %v", err)
	}

	polices := [][]string{
		// If the user is admin, no check takes place
		// we have these polices as placeholders
		{RoleAdmin, ObjectDatabase, ActionRead},
		{RoleAdmin, ObjectDatabase, ActionWrite},
		{RoleAdmin, ObjectRBAC, ActionWrite},
		{RoleAdmin, ObjectAuth, ActionWrite},

		{RoleDeveloper, ObjectDatabase, ActionRead},
		{RoleDeveloper, ObjectDatabase, ActionWrite},

		{RoleViewer, ObjectDatabase, ActionRead},
	}

	if _, err := Enforcer.AddPolicies(polices); err != nil {
		return fmt.Errorf("error adding rbac polices: %v", err)
	}

	// Update policies every 5 minutes
	go func() {
		time.Sleep(5 * time.Minute)
		Enforcer.LoadPolicy()
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
