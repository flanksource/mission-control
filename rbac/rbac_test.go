package rbac

import (
	"fmt"

	"github.com/casbin/casbin/v2"
	casbinModel "github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

func NewEnforcer(policyStr string) (*casbin.Enforcer, error) {
	model, err := casbinModel.NewModelFromString(rbac.DefaultModel)
	if err != nil {
		return nil, err
	}

	sa := stringadapter.NewAdapter(policyStr)
	e, err := casbin.NewEnforcer(model, sa)
	rbac.AddCustomFunctions(e)
	return e, err
}

var _ = ginkgo.Describe("Enforcer", func() {
	policies := `p, admin, *, * , allow,  true, na`

	var userID = uuid.New()

	permissions := []models.Permission{
		{
			ID:       uuid.New(),
			PersonID: lo.ToPtr(userID),
			Object:   policy.ObjectCatalog,
			Action:   policy.ActionRead,
		},
		{
			ID:       uuid.New(),
			PersonID: lo.ToPtr(userID),
			Object:   "*",
			Action:   policy.ActionRead,
		},
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

	var enforcer *casbin.Enforcer

	ginkgo.BeforeEach(func() {
		var err error
		enforcer, err = NewEnforcer(policies)
		Expect(err).To(BeNil())

		for _, p := range permissions {
			for _, rule := range adapter.PermissionToCasbinRule(p) {
				ok, err := enforcer.AddPolicy(lo.ToAnySlice(rule[1:])...)
				Expect(err).To(BeNil())
				Expect(ok).To(BeTrue())
			}
		}
	})

	for _, td := range testData {
		ginkgo.It(td.description, func() {
			allowed, err := enforcer.Enforce(td.user, td.obj, td.act)
			Expect(err).To(BeNil())
			Expect(allowed).To(Equal(td.allowed), fmt.Sprintf("user=%s, obj=%v, act=%s", td.user, td.obj, td.act))
		})
	}
})
