package policy

import "strings"

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
		Actions: ActionPlaybookApprove,
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
		Actions: ActionPlaybookRun,
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
				definitions = append(definitions, []string{acl.Principal, object, action[1:], "deny", "true", "na"})
			} else {
				definitions = append(definitions, []string{acl.Principal, object, action, "allow", "true", "na"})
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

const (
	// Roles
	RoleAdmin     = "admin"
	RoleEveryone  = "everyone"
	RoleEditor    = "editor"
	RoleViewer    = "viewer"
	RoleCommander = "commander"
	RoleResponder = "responder"
	RoleAgent     = "agent"

	// Objects
	ObjectKubernetesProxy  = "kubernetes-proxy"
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
	ObjectNotification     = "notification"
)

// Actions
const (
	ActionAll    = "*"
	ActionCRUD   = "create,read,update,delete"
	ActionCreate = "create"
	ActionDelete = "delete"
	ActionRead   = "read"
	ActionUpdate = "update"

	// Playbooks
	ActionPlaybookRun     = "playbook:run"
	ActionPlaybookApprove = "playbook:approve"
)

var AllActions = []string{
	ActionCreate,
	ActionDelete,
	ActionRead,
	ActionUpdate,
	ActionPlaybookApprove,
	ActionPlaybookRun,
}