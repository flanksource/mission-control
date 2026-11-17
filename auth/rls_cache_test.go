package auth

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
)

var _ = ginkgo.Describe("InvalidateRLSCacheForUser", func() {
	ginkgo.It("removes the payloads cached per access token along with the base payload", func() {
		const subject = "federated:3f1c0000-0000-0000-0000-000000000001"
		const other = subject + "2"

		keys := []string{
			getRLSCacheKey(subject),
			getRLSCacheKey(subject) + rbac.AccessTokenIDCacheKey(uuid.New()),
			getRLSCacheKey(other),
			getRLSCacheKey(other) + rbac.AccessTokenIDCacheKey(uuid.New()),
		}
		for _, key := range keys {
			tokenCache.SetDefault(key, &rls.Payload{})
		}
		ginkgo.DeferCleanup(func() {
			for _, key := range keys {
				tokenCache.Delete(key)
			}
		})

		InvalidateRLSCacheForUser(subject)

		for _, key := range keys[:2] {
			_, found := tokenCache.Get(key)
			Expect(found).To(BeFalse(), key)
		}
		for _, key := range keys[2:] {
			_, found := tokenCache.Get(key)
			Expect(found).To(BeTrue(), key)
		}
	})
})

var _ = ginkgo.Describe("The RLS payload of a playbook", func() {
	ginkgo.It("lists only the rows the playbook's read grants select, with no user on the context", func() {
		namespace := fmt.Sprintf("rls-playbook-%s", uuid.NewString()[:8])
		meta := func(name string) metav1.ObjectMeta {
			return metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())}
		}

		var readable, hidden models.ConfigItem
		for _, item := range []*models.ConfigItem{&readable, &hidden} {
			*item = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("rls-playbook-" + uuid.NewString()[:8]), ConfigClass: "Secret", Type: lo.ToPtr("Repro::Secret")}
			Expect(DefaultContext.DB().Create(item).Error).To(Succeed())
		}
		playbook := models.Playbook{ID: uuid.New(), Namespace: namespace, Name: "reader", Spec: []byte(`{}`), Source: models.SourceConfigFile}
		Expect(DefaultContext.DB().Create(&playbook).Error).To(Succeed())

		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: meta("readable"),
			Spec: v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{
				ScopeResourceRef: v1.ScopeResourceRef{Name: *readable.Name},
			}}}},
		})).To(Succeed())
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: meta("read"),
			Spec:       v1.RoleSpec{Rules: []v1.RoleRule{{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "readable"}}}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: meta("read"),
			Spec: v1.RoleBindingSpec{Role: "read", Subjects: v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{
				Playbooks: []v1.PermissionGroupSelector{{Name: playbook.Name, Namespace: playbook.Namespace}},
			}}},
		})).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
		FlushTokenCache()

		ginkgo.DeferCleanup(func() {
			for _, model := range []any{&models.RoleBinding{}, &models.Role{}, &models.Scope{}, &models.Playbook{}} {
				Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(model).Error).To(Succeed())
			}
			Expect(DefaultContext.DB().Delete(&readable).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(&hidden).Error).To(Succeed())
			Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
			FlushTokenCache()
		})

		var listed []uuid.UUID
		err := WithRLS(DefaultContext.WithSubject(playbook.ID.String()), func(ctx context.Context) error {
			return ctx.DB().Model(&models.ConfigItem{}).Where("id IN ?", []uuid.UUID{readable.ID, hidden.ID}).Pluck("id", &listed).Error
		})
		Expect(err).To(Succeed())
		Expect(listed).To(Equal([]uuid.UUID{readable.ID}))
	})
})
