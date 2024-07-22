package rbac

import (
	"fmt"
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/db"
	"gopkg.in/yaml.v3"
)

var enforcer *casbin.SyncedCachedEnforcer

func Init(ctx context.Context, adminUserID string) error {
	model, err := model.NewModelFromString(modelDefinition)
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

	policies := []Policy{
		{
			Principal: RoleEveryone,
			ACLs: []ACL{
				{
					Actions: "!*",
					Objects: strings.Join([]string{ObjectAuthConfidential}, ","),
				},
			},
		},
		{
			Inherit:   []string{RoleEveryone},
			Principal: RoleAdmin,
			ACLs:      []ACL{All("*")},
		}, {
			Principal: RoleViewer,
			ACLs: []ACL{Read(
				ObjectDatabasePublic,
				ObjectCanary,
				ObjectCatalog,
				ObjectPlaybooks,
				ObjectTopology)},
		},
		{
			Inherit:   []string{RoleViewer},
			Principal: RoleCommander,
			ACLs: []ACL{
				CRUD(ObjectIncident),
			},
		},
		{
			Inherit:   []string{RoleViewer},
			Principal: RoleResponder,
			ACLs: []ACL{
				CRUD(ObjectIncident),
			},
		},
		{
			Inherit:   []string{RoleViewer},
			Principal: RoleEditor,
			ACLs: []ACL{
				CRUD(ObjectCanary, ObjectCatalog, ObjectTopology, ObjectPlaybooks, ObjectKubernetesProxy),
				Run(ObjectPlaybooks),
				Approve(ObjectPlaybooks),
			},
		},

		{
			Principal: RoleAgent,
			ACLs: []ACL{
				Read(ObjectPlaybooks, ObjectDatabasePublic),
				Write(ObjectAgentPush),
			},
		},
	}
	data, _ := yaml.Marshal(policies)
	logger.Errorf(string(data))

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

	enforcer.EnableAutoSave(ctx.Properties().On(true, "casbin.auto.save"))
	enforcer.StartAutoLoadPolicy(ctx.Properties().Duration("casbin.cache.reload.interval", 5*time.Minute))

	return nil
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
	return enforcer.GetImplicitRolesForUser(user)
}

func PermsForUser(user string) string {
	perms, _ := enforcer.GetImplicitPermissionsForUser(user)
	s := ""
	for _, perm := range perms {
		if s != "" {
			s += "\n"
		}
		s += strings.Join(perm, ",")
	}
	return s
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
		ctx.Tracef("%s %s:%s = %v", subject, object, action, allowed)
	}

	return allowed
}
