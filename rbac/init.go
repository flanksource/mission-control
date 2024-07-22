package rbac

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/db"
	"gopkg.in/yaml.v3"
)

var enforcer *casbin.SyncedCachedEnforcer

//go:embed policies.yaml
var defaultPolicies string

//go:embed model.ini
var defaultModel string

func Init(ctx context.Context, adminUserID string) error {
	model, err := model.NewModelFromString(defaultModel)
	if err != nil {
		return fmt.Errorf("error creating rbac model: %v", err)
	}

	info := &db.Info{}
	if err := info.Get(ctx.DB()); err != nil {
		ctx.Warnf("Cannot get DB info: %v", err)
	}

	for _, table := range append(info.Views, info.Tables...) {
		if GetObjectByTable(table) == "" {
			ctx.Warnf("Unmapped database table: %s", table)
		}
	}
	for _, table := range info.Functions {
		if GetObjectByTable("rpc/"+table) == "" {
			ctx.Warnf("Unmapped database function: %s", table)
		}
	}

	db := ctx.DB()
	gormadapter.TurnOffAutoMigrate(db)
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		return fmt.Errorf("error creating rbac adapter: %v", err)
	}

	enforcer, err = casbin.NewSyncedCachedEnforcer(model, adapter)
	if err != nil {
		return fmt.Errorf("error creating rbac enforcer: %v", err)
	}
	if err := enforcer.LoadPolicy(); err != nil {
		ctx.Errorf("Failed to load existing policies: %v", err)
	}

	enforcer.SetExpireTime(ctx.Properties().Duration("casbin.cache.expiry", 1*time.Minute))
	enforcer.EnableCache(ctx.Properties().On(true, "casbin.cache"))
	if ctx.Properties().Int("casbin.log.level", 1) >= 2 {
		enforcer.EnableLog(true)
	}

	if adminUserID != "" {
		if _, err := enforcer.AddRoleForUser(adminUserID, RoleAdmin); err != nil {
			return fmt.Errorf("error adding role for admin user: %v", err)
		}
	}

	var policies []Policy

	if err := yaml.Unmarshal([]byte(defaultPolicies), &policies); err != nil {
		return fmt.Errorf("unable to load default policies: %v", err)
	}

	enforcer.EnableAutoSave(ctx.Properties().On(true, "casbin.auto.save"))

	// Adding policies in a loop is important
	// If we use Enforcer.AddPolicies(), new policies do not get saved
	for _, p := range policies {
		for _, inherited := range p.Inherit {
			if _, err := enforcer.AddGroupingPolicy(p.Principal, inherited); err != nil {
				return fmt.Errorf("error adding group policy for %s -> %s: %v", p.Principal, inherited, err)
			}
		}
		for _, acl := range p.GetPolicyDefintions() {
			if _, err := enforcer.AddPolicy(acl); err != nil {
				return fmt.Errorf("error adding rbac policy %s: %v", p, err)
			}
		}
	}

	enforcer.StartAutoLoadPolicy(ctx.Properties().Duration("casbin.cache.reload.interval", 5*time.Minute))

	return nil
}

func Stop() {
	if enforcer != nil {
		enforcer.StopAutoLoadPolicy()
	}
}
func DeleteRoleForUser(user string, role string) error {
	_, err := enforcer.DeleteRoleForUser(user, role)
	return err

}

func AddRoleForUser(user string, role ...string) error {
	_, err := enforcer.AddRolesForUser(user, role)
	return err
}

func RolesForUser(user string) ([]string, error) {
	implicit, err := enforcer.GetImplicitRolesForUser(user)
	if err != nil {
		return nil, err
	}

	roles, err := enforcer.GetRolesForUser(user)
	if err != nil {
		return nil, err
	}

	return append(implicit, roles...), nil
}

func PermsForUser(user string) ([]Permission, error) {
	implicit, err := enforcer.GetImplicitPermissionsForUser(user)
	if err != nil {
		return nil, err
	}
	perms, err := enforcer.GetPermissionsForUser(user)
	if err != nil {
		return nil, err
	}
	var s []Permission
	for _, perm := range append(perms, implicit...) {
		s = append(s, NewPermission(perm))
	}
	return s, nil
}

func Check(ctx context.Context, subject, object, action string) bool {
	hasEveryone, err := enforcer.HasRoleForUser(subject, RoleEveryone)
	if err != nil {
		ctx.Errorf("RBAC Enforce failed: %v", err)
		return false
	}

	if !hasEveryone {
		if _, err := enforcer.AddRoleForUser(subject, RoleEveryone); err != nil {
			ctx.Debugf("error adding role %s to user %s", RoleEveryone, subject)
		}
	}

	if ctx.Properties().On(false, "casbin.explain") {
		allowed, rules, err := enforcer.EnforceEx(subject, object, action)
		if err != nil {
			ctx.Errorf("RBAC Enforce failed: %v", err)
		}
		ctx.Debugf("[%s] %s:%s -> %v (%s)", subject, object, action, allowed, strings.Join(rules, "\n\t"))
		return allowed
	}

	allowed, err := enforcer.Enforce(subject, object, action)
	if err != nil {
		ctx.Errorf("RBAC Enforce failed: %v", err)
		return false
	}
	if ctx.IsTrace() {
		ctx.Tracef("rbac: %s %s:%s = %v", subject, object, action, allowed)
	}

	return allowed
}
