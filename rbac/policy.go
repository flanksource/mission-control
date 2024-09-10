package rbac

import (
	"fmt"
	"strings"
)

func Read(objects ...string) ACL {
	return ACL{
		Actions: ActionRead,
		Objects: strings.Join(objects, ","),
	}
}
func Update(objects ...string) ACL {
	return ACL{
		Actions: ActionUpdate,
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
	ACLs      []ACL    `yaml:"acl,omitempty" json:"acl"`
	Inherit   []string `yaml:"inherit,omitempty" json:"inherit"`
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

type Permission struct {
	Subject string `json:"subject,omitempty"`
	Object  string `json:"object,omitempty"`
	Action  string `json:"action,omitempty"`
	Deny    bool   `json:"deny,omitempty"`
}

func NewPermission(perm []string) Permission {
	return Permission{
		Subject: perm[0],
		Object:  perm[1],
		Action:  perm[2],
		Deny:    perm[3] == "deny",
	}
}

func NewPermissions(perms [][]string) []Permission {
	var arr []Permission

	for _, p := range perms {
		arr = append(arr, NewPermission(p))
	}

	return arr

}

func (p Permission) String() string {
	return fmt.Sprintf("%s on %s (%s)", p.Subject, p.Object, p.Action)
}

const (

	// Roles
	RoleAdmin     = "admin"
	RoleEveryone  = "everyone"
	RoleEditor    = "editor"
	RoleViewer    = "viewer"
	RoleCommander = "commander"
	RoleResponder = "responder"
	RoleAgent     = "agent"

	// Actions
	ActionRead            = "read"
	ActionUpdate          = "update"
	ActionCreate          = "create"
	ActionDelete          = "delete"
	ActionRun             = "run"
	ActionApprove         = "approve"
	ActionAll             = "*"
	ActionCRUD            = "create,read,update,delete"
	ObjectKubernetesProxy = "kubernetes-proxy"
	// Objects
	ObjectLogs             = "logs"
	ObjectAgent            = "agent"
	ObjectAgentPush        = "agent-push"
	ObjectArtifact         = "artifact"
	ObjectAuth             = "auth"
	ObjectCanary           = "canaries"
	ObjectCatalog          = "catalog"
	ObjectConnection       = "connection"
	ObjectConnectionDetail = "connection-detail"
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
	ObjectPeople           = "people"

	ObjectNotification = "notification"
)

var (
	AllActions = []string{ActionApprove, ActionCreate, ActionRead, ActionRun, ActionUpdate, ActionDelete}
)
