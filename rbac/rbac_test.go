package rbac

import (
	"fmt"
	"strings"
	"testing"

	"github.com/casbin/casbin/v2"
	casbinModel "github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

func NewEnforcer(policy string) (*casbin.Enforcer, error) {
	model, err := casbinModel.NewModelFromString(defaultModel)
	if err != nil {
		return nil, err
	}

	sa := stringadapter.NewAdapter(policy)
	e, err := casbin.NewEnforcer(model, sa)
	addCustomFunctions(e)
	return e, err
}

func TestEnforcer(t *testing.T) {
	policy := `
p, admin, *, * , allow, true, 1
g, johndoe, admin, , ,       , 1
p, johndoe, *, playbook:run, allow, r.obj.playbook.name == 'scale-deployment' , 1
p, johndoe, *, playbook:run, deny, r.obj.playbook.name == 'delete-deployment' , 1
p, johndoe, *, playbook:run, allow, r.obj.playbook.name == 'restart-deployment' && r.obj.config.tags.namespace == 'default' , 1
p, alice, *, playbook:run, deny, r.obj.playbook.name == 'restart-deployment' && r.obj.config.tags.namespace == 'default', 1
`

	var userID = uuid.New()

	permissions := []models.Permission{
		{
			ID:       uuid.New(),
			PersonID: lo.ToPtr(userID),
			Object:   ObjectCatalog,
			Action:   ActionRead,
			Tags: map[string]string{
				"namespace": "default",
				"cluster":   "aws",
			},
			Agents: []string{"123"},
		},
		{
			ID:       uuid.New(),
			PersonID: lo.ToPtr(userID),
			Object:   "*",
			Action:   ActionRead,
			Tags: map[string]string{
				"namespace": "default",
			},
		},
	}

	for _, p := range permissions {
		_policy := strings.Join(adapter.PermissionToCasbinRule(p), ",")
		policy += fmt.Sprintf("\n%s", _policy)
	}

	enforcer, err := NewEnforcer(policy)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		description string
		user        string
		obj         any
		act         string
		allowed     bool
	}{
		{
			description: "simple | allow",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "scale-deployment"}}.AsMap(),
			act:         "playbook:run",
			allowed:     true,
		},
		{
			description: "simple | explicit deny",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "delete-deployment"}}.AsMap(),
			act:         "playbook:run",
			allowed:     false,
		},
		{
			description: "simple | default deny",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "delete-namespace"}}.AsMap(),
			act:         "playbook:run",
			allowed:     false,
		},
		{
			description: "multi | allow",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "restart-deployment"}, Config: models.ConfigItem{Tags: map[string]string{"namespace": "default"}}}.AsMap(),
			act:         "playbook:run",
			allowed:     true,
		},
		{
			description: "multi | explicit deny",
			user:        "alice",
			obj:         ABACResource{Playbook: models.Playbook{Name: "restart-deployment"}, Config: models.ConfigItem{Tags: map[string]string{"namespace": "default"}}}.AsMap(),
			act:         "playbook:run",
			allowed:     false,
		},
		{
			description: "test",
			user:        userID.String(),
			obj:         "catalog",
			act:         "read",
			allowed:     true,
		},
		{
			description: "abac catalog test",
			user:        userID.String(),
			obj:         ABACResource{Config: models.ConfigItem{Tags: map[string]string{"namespace": "default"}}}.AsMap(),
			act:         "read",
			allowed:     true,
		},
	}

	for _, td := range testData {
		t.Run(td.description, func(t *testing.T) {
			user := td.user
			obj := td.obj
			act := td.act

			allowed, err := enforcer.Enforce(user, obj, act)
			if err != nil {
				t.Fatal(err)
			}

			if allowed != td.allowed {
				t.Errorf("expected %t but got %t. user=%s, obj=%v, act=%s", td.allowed, allowed, user, obj, act)
			}
		})
	}
}
