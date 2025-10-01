package echo

import (
	"net/url"
	"os"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("permissionsToPostgRESTParams", Ordered, func() {
	var (
		guestUser models.Person
	)

	type testCase struct {
		name             string
		permissionPath   string
		expectedURLQuery map[string]string
	}

	BeforeAll(func() {
		if err := dutyRBAC.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter); err != nil {
			Fail("Failed to initialize RBAC: " + err.Error())
		}

		guestUser = models.Person{
			ID:    uuid.New(),
			Name:  "Guest User",
			Email: "guest@test.com",
		}
		err := DefaultContext.DB().Create(&guestUser).Error
		Expect(err).To(BeNil())

		err = dutyRBAC.AddRoleForUser(guestUser.ID.String(), policy.RoleGuest)
		Expect(err).To(BeNil())
	})

	AfterAll(func() {
		_, err := dutyRBAC.Enforcer().DeleteRolesForUser(guestUser.ID.String())
		Expect(err).To(BeNil())
		_, err = dutyRBAC.Enforcer().DeletePermissionsForUser(guestUser.ID.String())
		Expect(err).To(BeNil())
		err = DefaultContext.DB().Delete(&guestUser).Error
		Expect(err).To(BeNil())
	})

	DescribeTable("permission filtering",
		func(tc testCase) {
			yamlData, err := os.ReadFile(tc.permissionPath)
			Expect(err).To(BeNil())

			var permissionCRD v1.Permission
			err = yaml.Unmarshal(yamlData, &permissionCRD)
			Expect(err).To(BeNil())

			permissionCRD.UID = types.UID(uuid.New().String())

			err = db.PersistPermissionFromCRD(DefaultContext, &permissionCRD)
			Expect(err).To(BeNil())

			var permission models.Permission
			err = DefaultContext.DB().Where("name = ? AND namespace = ?", permissionCRD.Name, permissionCRD.Namespace).First(&permission).Error
			Expect(err).To(BeNil())

			_, err = dutyRBAC.Enforcer().AddPermissionsForUser(guestUser.ID.String(),
				[]string{policy.ObjectPlaybooks, permission.Action, permission.Effect(), "true", permission.ID.String()},
			)
			Expect(err).To(BeNil())

			ctx := DefaultContext.WithUser(&guestUser)

			q := url.Values{}
			err = permissionsToPostgRESTParams(ctx, q, PermissionFilterConfig{
				ResourceType: ResourceTypePlaybooks,
				Fields: []ResourceSelectorField{
					ResourceSelectorID,
					ResourceSelectorNamespace,
					ResourceSelectorName,
				},
			})
			Expect(err).To(BeNil())

			for key, expectedValue := range tc.expectedURLQuery {
				Expect(q.Get(key)).To(Equal(expectedValue))
			}

			_, err = dutyRBAC.Enforcer().DeletePermissionsForUser(guestUser.ID.String())
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Delete(&permission).Error
			Expect(err).To(BeNil())
		},
		Entry("filter by playbook names", testCase{
			name:           "should apply playbook name filter",
			permissionPath: "testdata/permission-playbook-by-name.yaml",
			expectedURLQuery: map[string]string{
				"name": "in.(backup-postgres,loki-logs)",
			},
		}),
		Entry("filter by playbook IDs", testCase{
			name:           "should apply playbook ID filter",
			permissionPath: "testdata/permission-playbook-by-id.yaml",
			expectedURLQuery: map[string]string{
				"id": "in.(018c3071-d8d7-7f62-86c0-f80e202c03dd,018c3071-d8d7-7f62-86c0-f80e202c03ee)",
			},
		}),
		Entry("filter by playbook namespace", testCase{
			name:           "should apply playbook namespace filter",
			permissionPath: "testdata/permission-playbook-by-namespace.yaml",
			expectedURLQuery: map[string]string{
				"namespace": "in.(default)",
			},
		}),
	)
})
