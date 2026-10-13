package permissions_test

import (
	"errors"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = ginkgo.Describe("Permissions that deny", ginkgo.Ordered, func() {
	const namespace = "permission-deny"

	var guest *models.Person

	ginkgo.BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())
		guest = setup.CreateUserWithRole(DefaultContext, "Permission Deny Guest", "permission-deny-guest@test.com", policy.RoleGuest)
	})

	ginkgo.AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Permission{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(guest).Error).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	updateCatalog := func(name string, deny bool) *v1.Permission {
		return &v1.Permission{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.PermissionSpec{
				Subject: v1.PermissionSubject{Person: guest.Email},
				Actions: []string{policy.ActionUpdate},
				Object:  v1.PermissionObject{Selectors: rbac.Selectors{Configs: []types.ResourceSelector{{Name: "*"}}}},
				Deny:    deny,
			},
		}
	}

	canUpdateCatalog := func() bool {
		ginkgo.GinkgoHelper()
		Expect(rbac.ReloadPolicy()).To(Succeed())
		return rbac.Check(DefaultContext, guest.ID.String(), policy.ObjectCatalog, policy.ActionUpdate)
	}

	ginkgo.It("applies a Permission with deny: false", func() {
		permission := updateCatalog("deny-false", false)
		Expect(db.PersistPermissionFromCRD(DefaultContext, permission)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Where("id = ?", permission.UID).Delete(&models.Permission{}).Error).To(Succeed())
		})

		Expect(canUpdateCatalog()).To(BeTrue())
	})

	ginkgo.It("makes a Permission with deny: true Invalid, granting nothing", func() {
		permission := updateCatalog("deny-true", false)
		Expect(db.PersistPermissionFromCRD(DefaultContext, permission)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Where("id = ?", permission.UID).Delete(&models.Permission{}).Error).To(Succeed())
		})
		Expect(canUpdateCatalog()).To(BeTrue())

		// Rewritten in Kubernetes with deny set, the allow stored for it goes too
		permission.Spec.Deny = true
		err := db.PersistPermissionFromCRD(DefaultContext, permission)
		var notReady *kopper.NotReadyError
		Expect(errors.As(err, &notReady)).To(BeTrue(), "%v", err)
		Expect(notReady.Reason).To(Equal(kopper.ReasonInvalid))
		Expect(notReady.Message).To(ContainSubstring("deny isn't supported"))

		Expect(canUpdateCatalog()).To(BeFalse())
	})

	ginkgo.It("deletes Permissions stored with deny at startup", func() {
		denying := models.Permission{
			ID: uuid.New(), Name: "stored-deny", Namespace: namespace, Action: policy.ActionUpdate, Object: policy.ObjectCatalog,
			Subject: guest.ID.String(), SubjectType: models.PermissionSubjectTypePerson, Deny: true,
		}
		allowing := models.Permission{
			ID: uuid.New(), Name: "stored-allow", Namespace: namespace, Action: policy.ActionDelete, Object: policy.ObjectCatalog,
			Subject: guest.ID.String(), SubjectType: models.PermissionSubjectTypePerson,
		}
		Expect(DefaultContext.DB().Create([]*models.Permission{&denying, &allowing}).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Delete([]*models.Permission{&denying, &allowing}).Error).To(Succeed())
		})

		Expect(db.DeleteDenyRules(DefaultContext)).To(Succeed())

		var stored []models.Permission
		Expect(DefaultContext.DB().Where("id IN ? AND deleted_at IS NULL", []uuid.UUID{denying.ID, allowing.ID}).Find(&stored).Error).To(Succeed())
		Expect(stored).To(HaveLen(1))
		Expect(stored[0].ID).To(Equal(allowing.ID))

		Expect(canUpdateCatalog()).To(BeFalse())
		Expect(rbac.Check(DefaultContext, guest.ID.String(), policy.ObjectCatalog, policy.ActionDelete)).To(BeTrue())
	})

	ginkgo.It("loads no casbin deny policy from Permissions", func() {
		permission := updateCatalog("loaded", false)
		Expect(db.PersistPermissionFromCRD(DefaultContext, permission)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Where("id = ?", permission.UID).Delete(&models.Permission{}).Error).To(Succeed())
		})
		Expect(rbac.ReloadPolicy()).To(Succeed())

		policies, err := rbac.Enforcer().GetPolicy()
		Expect(err).ToNot(HaveOccurred())
		Expect(policies).To(ContainElement(HaveExactElements(guest.ID.String(), policy.ObjectCatalog, policy.ActionUpdate, "allow", "", string(permission.UID))))
		for _, p := range policies {
			// duty's built-in policies (id "na") still deny everyone the Kratos tables, until they're removed from duty
			if p[3] == "deny" {
				Expect(p[1]).To(Equal(policy.ObjectAuthConfidential), "%v", p)
				Expect(p[5]).To(Equal("na"), "%v", p)
			}
		}
	})
})
