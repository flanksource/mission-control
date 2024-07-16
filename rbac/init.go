package rbac

import (
	"fmt"
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/duty/context"
)

func Read(objects ...string) ACL {
	return ACL{
		Actions: ActionRead,
		Objects: strings.Join(objects, ","),
	}
}
func Write(objects ...string) ACL {
	return ACL{
		Actions: ActionWrite,
		Objects: strings.Join(objects, ","),
	}
}
func Approve(objects ...string) ACL {
	return ACL{
		Actions: ActionApprove,
		Objects: strings.Join(objects, ","),
	}
}

func Create(objects ...string) ACL {
	return ACL{
		Actions: ActionCreate,
		Objects: strings.Join(objects, ","),
	}
}
func Delete(objects ...string) ACL {
	return ACL{
		Actions: ActionDelete,
		Objects: strings.Join(objects, ","),
	}
}
func CRUD(objects ...string) ACL {
	return ACL{
		Actions: ActionCRUD,
		Objects: strings.Join(objects, ","),
	}
}
func Run(objects ...string) ACL {
	return ACL{
		Actions: ActionRun,
		Objects: strings.Join(objects, ","),
	}
}

func All(objects ...string) ACL {
	return ACL{
		Actions: ActionAll,
		Objects: strings.Join(objects, ","),
	}
}

type ACL struct {
	Objects   string `yaml:"objects" json:"objects"`
	Actions   string `yaml:"actions" json:"actions"`
	Principal string `yaml:"principal,omitempty" json:"principal,omitempty"`
}

func (acl ACL) GetPolicyDefinition() [][]string {
	var definitions [][]string
	for _, object := range strings.Split(acl.Objects, ",") {
		for _, action := range strings.Split(acl.Actions, ",") {
			if strings.HasPrefix(action, "!") {
				definitions = append(definitions, []string{acl.Principal, object, action[1:], "deny"})
			} else {
				definitions = append(definitions, []string{acl.Principal, object, action, "allow"})
			}
		}
	}
	return definitions
}

type Policy struct {
	Principal string   `yaml:"principal" json:"principal"`
	ACLs      []ACL    `yaml:"acl" json:"acl"`
	Inherit   []string `yaml:"inherit" json:"inherit"`
}

func (p Policy) GetPolicyDefintions() [][]string {
	var definitions [][]string
	for _, acl := range p.ACLs {
		if acl.Principal == "" {
			acl.Principal = p.Principal
		}
		definitions = append(definitions, acl.GetPolicyDefinition()...)
	}
	return definitions
}

func (p Policy) String() string {
	s := ""
	for _, policy := range p.GetPolicyDefintions() {
		if s != "" {
			s += "\n"
		}
		s += strings.Join(policy, ", ")
	}
	return s
}

const (
	modelDefinition = `
    [request_definition]
    r = sub, obj, act

    [policy_definition]
    p = sub, obj, act, eft

    [role_definition]
    g = _, _

    [policy_effect]
		e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

    [matchers]
    m = g(r.sub, p.sub) && ( p.obj == '*' || r.obj == p.obj) && (p.act == '*' || r.act == p.act)`

	// Roles
	RoleAdmin     = "admin"
	RoleEveryone  = "everyone"
	RoleEditor    = "editor"
	RoleViewer    = "viewer"
	RoleCommander = "commander"
	RoleResponder = "responder"
	RoleAgent     = "agent"

	// Actions
	ActionRead    = "read"
	ActionWrite   = "write"
	ActionCreate  = "create"
	ActionDelete  = "delete"
	ActionRun     = "run"
	ActionApprove = "approve"
	ActionAll     = "*"
	ActionCRUD    = "create,read,update,delete"

	// Objects
	ObjectLogs             = "logs"
	ObjectAgent            = "agent"
	ObjectAgentPush        = "agent-push"
	ObjectArtifact         = "artifact"
	ObjectAuth             = "auth"
	ObjectCanary           = "canaries"
	ObjectCatalog          = "catalog"
	ObjectConnection       = "connection"
	ObjectDatabase         = "database"
	ObjectDatabaseIdentity = "database.identities"
	ObjectAuthConfidential = "database.kratos"
	ObjectDatabasePublic   = "database.public"
	ObjectDatabaseSettings = "database.config_scrapers"
	ObjectDatabaseSystem   = "database.system"
	ObjectIncident         = "incident"
	ObjectMonitor          = "database.monitor"
	ObjectPlaybooks        = "playbooks"
	ObjectRBAC             = "rbac"
	ObjectTopology         = "topology"
)

var (
	AllActions = []string{ActionApprove, ActionCreate, ActionRead, ActionRun, ActionWrite, ActionDelete}
)
var Enforcer *casbin.SyncedCachedEnforcer

func Init(ctx context.Context, adminUserID string) error {
	model, err := model.NewModelFromString(modelDefinition)
	db := ctx.DB()
	if err != nil {
		return fmt.Errorf("error creating rbac model: %v", err)
	}

	gormadapter.TurnOffAutoMigrate(db)
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		return fmt.Errorf("error creating rbac adapter: %v", err)
	}

	Enforcer, err = casbin.NewSyncedCachedEnforcer(model, adapter)
	Enforcer.SetExpireTime(ctx.Properties().Duration("casbin.cache.expiry", 1*time.Minute))
	Enforcer.EnableCache(ctx.Properties().On("casbin.cache"))
	if err != nil {
		return fmt.Errorf("error creating rbac enforcer: %v", err)
	}
	if ctx.Properties().Int("casbin.log.level", 1) >= 2 {
		Enforcer.EnableLog(true)
	}

	if adminUserID != "" {
		if _, err := Enforcer.AddRoleForUser(adminUserID, RoleAdmin); err != nil {
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
				CRUD(ObjectCanary, ObjectCatalog, ObjectTopology, ObjectPlaybooks),
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

	// Adding policies in a loop is important
	// If we use Enforcer.AddPolicies(), new policies do not get saved
	for _, p := range policies {
		for _, inherited := range p.Inherit {
			if _, err := Enforcer.AddGroupingPolicy(p.Principal, inherited); err != nil {
				return fmt.Errorf("error adding group policy for %s -> %s: %v", p.Principal, inherited, err)
			}
		}
		for _, acl := range p.GetPolicyDefintions() {
			if _, err := Enforcer.AddPolicy(acl); err != nil {
				return fmt.Errorf("error adding rbac policy %s: %v", p, err)
			}
		}
	}

	Enforcer.StartAutoLoadPolicy(ctx.Properties().Duration("cache.reload.interval", 5*time.Minute))

	return nil
}

func PermsForUser(user string) string {
	perms, _ := Enforcer.GetImplicitPermissionsForUser(user)
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
	hasEveryone, err := Enforcer.HasRoleForUser(subject, RoleEveryone)

	if err != nil {
		ctx.Errorf("RBAC Enforce failed: %v", err)
		return false
	}
	if !hasEveryone {
		Enforcer.AddRoleForUser(subject, RoleEveryone)
	}

	if ctx.Properties().On("casbin.explain") {
		allowed, rules, err := Enforcer.EnforceEx(subject, object, action)
		if err != nil {
			ctx.Errorf("RBAC Enforce failed: %v", err)
		}
		ctx.Debugf("[%s] %s:%s -> %s (%s)", subject, object, action, allowed, strings.Join(rules, "\n\t"))
		return allowed
	}

	allowed, err := Enforcer.Enforce(subject, object, action)
	if err != nil {
		ctx.Errorf("RBAC Enforce failed: %v", err)
		return false
	}
	if ctx.IsTrace() {
		ctx.Tracef("%s %s:%s = %v", subject, object, action, allowed)
	}
	return allowed
}
