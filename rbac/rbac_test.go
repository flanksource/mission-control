package rbac

import (
	"testing"

	"github.com/casbin/casbin/v2"
	casbinModel "github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"github.com/flanksource/duty/models"
)

func NewEnforcer(policy string) (*casbin.Enforcer, error) {
	model, err := casbinModel.NewModelFromString(defaultModel)
	if err != nil {
		return nil, err
	}

	sa := stringadapter.NewAdapter(policy)
	return casbin.NewEnforcer(model, sa)
}

func TestEnforcer(t *testing.T) {
	policy := `
p, johndoe, *, playbook:run, r.obj.playbook.name == 'scale-deployment', allow
p, johndoe, *, playbook:run, r.obj.playbook.name == 'delete-deployment', deny
p, johndoe, *, playbook:run, r.obj.playbook.name == 'restart-deployment' && r.obj.config.tags.namespace == 'default', allow
p, alice, *, playbook:run, r.obj.playbook.name == 'restart-deployment' && r.obj.config.tags.namespace == 'default', deny
`

	enforcer, err := NewEnforcer(policy)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		description string
		user        string
		obj         ABACResource
		act         string
		allowed     bool
	}{
		{
			description: "simple | allow",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "scale-deployment"}},
			act:         "playbook:run",
			allowed:     true,
		},
		{
			description: "simple | explicit deny",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "delete-deployment"}},
			act:         "playbook:run",
			allowed:     false,
		},
		{
			description: "simple | default deny",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "delete-namespace"}},
			act:         "playbook:run",
			allowed:     false,
		},
		{
			description: "multi | allow",
			user:        "johndoe",
			obj:         ABACResource{Playbook: models.Playbook{Name: "restart-deployment"}, Config: models.ConfigItem{Tags: map[string]string{"namespace": "default"}}},
			act:         "playbook:run",
			allowed:     true,
		},
		{
			description: "multi | explicit deny",
			user:        "alice",
			obj:         ABACResource{Playbook: models.Playbook{Name: "restart-deployment"}, Config: models.ConfigItem{Tags: map[string]string{"namespace": "default"}}},
			act:         "playbook:run",
			allowed:     false,
		},
	}

	for _, td := range testData {
		t.Run(td.description, func(t *testing.T) {
			user := td.user
			obj := td.obj
			act := td.act

			allowed, err := enforcer.Enforce(user, obj.AsMap(), act)
			if err != nil {
				t.Fatal(err)
			}

			if allowed != td.allowed {
				t.Errorf("expected %t for %s, %s, %s but got %t", td.allowed, user, obj.AsMap(), act, allowed)
			}
		})
	}
}
