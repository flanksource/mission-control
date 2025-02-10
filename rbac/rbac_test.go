package rbac

import (
	"testing"

	"github.com/casbin/casbin/v2"
	casbinModel "github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

func NewEnforcer(policy string) (*casbin.Enforcer, error) {
	model, err := casbinModel.NewModelFromString(rbac.DefaultModel)
	if err != nil {
		return nil, err
	}

	sa := stringadapter.NewAdapter(policy)
	e, err := casbin.NewEnforcer(model, sa)
	rbac.AddCustomFunctions(e)
	return e, err
}

func TestEnforcer(t *testing.T) {
	policies := `p, admin, *, * , allow,  true, na`

	var userID = uuid.New()

	permissions := []models.Permission{
		{
			ID:       uuid.New(),
			PersonID: lo.ToPtr(userID),
			Object:   policy.ObjectCatalog,
			Action:   policy.ActionRead,
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
			Action:   policy.ActionRead,
			Tags: map[string]string{
				"namespace": "default",
			},
		},
	}

	enforcer, err := NewEnforcer(policies)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range permissions {
		for _, policy := range adapter.PermissionToCasbinRule(p) {
			if ok, err := enforcer.AddPolicy(policy[1:]); err != nil || !ok {
				t.Fatal()
			}
		}
	}

	testData := []struct {
		description string
		user        string
		obj         any
		act         string
		allowed     bool
	}{
		{
			description: "abac catalog test",
			user:        userID.String(),
			obj:         &models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "default"}}},
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
