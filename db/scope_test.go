package db

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("Scope Persistence", func() {
	ginkgo.Context("PersistScopeFromCRD", func() {
		ginkgo.It("should persist a valid Scope CRD", func() {
			scopeObj := &v1.Scope{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "mission-control.flanksource.com/v1",
					Kind:       "Scope",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-scope",
					Namespace: "default",
					UID:       k8sTypes.UID(uuid.New().String()),
				},
				Spec: v1.ScopeSpec{
					Description: "Test scope",
					Targets: []v1.ScopeTarget{
						{
							Config: &types.ResourceSelector{
								Name:        "prod",
								Agent:       "homelab",
								TagSelector: "env=prod",
							},
						},
					},
				},
			}

			err := PersistScopeFromCRD(DefaultContext, scopeObj)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was saved as written, without an error
			var saved models.Scope
			err = DefaultContext.DB().Where("id = ?", scopeObj.UID).First(&saved).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(saved.Name).To(Equal("test-scope"))
			Expect(saved.Namespace).To(Equal("default"))
			Expect(saved.Description).To(Equal("Test scope"))
			Expect(saved.Error).To(BeNil())
			Expect(saved.ErrorReason).To(BeNil())

			// Verify targets JSON
			var targets []v1.ScopeTarget
			err = json.Unmarshal(saved.Targets, &targets)
			Expect(err).ToNot(HaveOccurred())
			Expect(targets).To(HaveLen(1))
			Expect(targets[0].Config).ToNot(BeNil())
			Expect(targets[0].Config.Name).To(Equal("prod"))
			Expect(targets[0].Config.Agent).To(Equal("homelab"), "the agent is stored as written, and resolved on every validation")
			Expect(targets[0].Config.TagSelector).To(Equal("env=prod"))
		})

		for i, tt := range []struct {
			name   string
			target v1.ScopeTarget
		}{
			{"an empty selector", v1.ScopeTarget{Config: &types.ResourceSelector{}}},
			{"a name pattern", v1.ScopeTarget{Config: &types.ResourceSelector{Name: "prod-*"}}},
			{"a field the type doesn't have", v1.ScopeTarget{Playbook: &types.ResourceSelector{TagSelector: "purpose=remediation"}}},
			{"a query option", v1.ScopeTarget{Config: &types.ResourceSelector{Name: "api", Search: "type=Pod"}}},
			{"a playbook field other than category", v1.ScopeTarget{Playbook: &types.ResourceSelector{FieldSelector: "title=Restart"}}},
			{"a malformed tagSelector", v1.ScopeTarget{Config: &types.ResourceSelector{TagSelector: "env in (prod"}}},
			{"two resource types in one target", v1.ScopeTarget{Config: &types.ResourceSelector{Name: "*"}, Playbook: &types.ResourceSelector{Name: "*"}}},
			{"a wildcard namespace", v1.ScopeTarget{Config: &types.ResourceSelector{Namespace: "*"}}},
			{"an agent that doesn't exist", v1.ScopeTarget{Config: &types.ResourceSelector{Agent: "no-such-agent"}}},
		} {
			ginkgo.It("stores a scope with "+tt.name+" as invalid", func() {
				scopeObj := &v1.Scope{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("invalid-scope-%d", i), Namespace: "default", UID: k8sTypes.UID(uuid.New().String())},
					Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{tt.target}},
				}
				Expect(PersistScopeFromCRD(DefaultContext, scopeObj)).ToNot(Succeed())

				var saved models.Scope
				Expect(DefaultContext.DB().Where("id = ?", scopeObj.UID).First(&saved).Error).To(Succeed())
				Expect(saved.Error).ToNot(BeNil())
				Expect(saved.ErrorReason).ToNot(BeNil())
			})
		}

		ginkgo.It("records why a scope is invalid", func() {
			scopeObj := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: "missing-agent", Namespace: "default", UID: k8sTypes.UID(uuid.New().String())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &types.ResourceSelector{Agent: "no-such-agent"}}}},
			}
			Expect(PersistScopeFromCRD(DefaultContext, scopeObj)).ToNot(Succeed())

			var saved models.Scope
			Expect(DefaultContext.DB().Where("id = ?", scopeObj.UID).First(&saved).Error).To(Succeed())
			Expect(*saved.ErrorReason).To(Equal("AgentNotFound"))
			Expect(*saved.Error).To(ContainSubstring("no-such-agent"))
		})

		ginkgo.It("resolves an agent id as well as a name", func() {
			scopeObj := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-by-id", Namespace: "default", UID: k8sTypes.UID(uuid.New().String())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &types.ResourceSelector{Agent: dummy.HomelabAgent.ID.String()}}}},
			}
			Expect(PersistScopeFromCRD(DefaultContext, scopeObj)).To(Succeed())
		})

		ginkgo.It("should fail with invalid UID", func() {
			scopeObj := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bad-uid-scope",
					Namespace: "default",
					UID:       "not-a-valid-uuid",
				},
				Spec: v1.ScopeSpec{
					Targets: []v1.ScopeTarget{
						{
							Config: &types.ResourceSelector{Name: "test"},
						},
					},
				},
			}

			err := PersistScopeFromCRD(DefaultContext, scopeObj)
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Context("DeleteScope", func() {
		ginkgo.It("should soft delete a Scope", func() {
			// Create a scope first
			scopeID := uuid.New()
			targetsJSON, _ := json.Marshal([]v1.ScopeTarget{
				{
					Config: &types.ResourceSelector{Name: "test"},
				},
			})
			scope := models.Scope{
				ID:        scopeID,
				Name:      "delete-me",
				Namespace: "default",
				Targets:   types.JSON(targetsJSON),
				Source:    models.SourceCRD,
			}
			err := DefaultContext.DB().Create(&scope).Error
			Expect(err).ToNot(HaveOccurred())

			// Delete it
			err = DeleteScope(DefaultContext, scopeID.String())
			Expect(err).ToNot(HaveOccurred())

			// Verify soft delete
			var deleted models.Scope
			err = DefaultContext.DB().Unscoped().Where("id = ?", scopeID).First(&deleted).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted.DeletedAt).ToNot(BeNil())
		})
	})

	ginkgo.Context("DeleteStaleScope", func() {
		ginkgo.It("should delete old scopes with same name/namespace", func() {
			oldID := uuid.New()
			newID := uuid.New()

			// Create old scope
			targetsJSON, _ := json.Marshal([]v1.ScopeTarget{
				{
					Config: &types.ResourceSelector{Name: "old"},
				},
			})
			oldScope := models.Scope{
				ID:        oldID,
				Name:      "my-scope",
				Namespace: "default",
				Targets:   types.JSON(targetsJSON),
				Source:    models.SourceCRD,
			}
			err := DefaultContext.DB().Create(&oldScope).Error
			Expect(err).ToNot(HaveOccurred())

			// Create new scope CRD
			newScopeCRD := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-scope",
					Namespace: "default",
					UID:       k8sTypes.UID(newID.String()),
				},
				Spec: v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &types.ResourceSelector{Name: "new"}}}},
			}

			// Delete stale
			err = DeleteStaleScope(DefaultContext, newScopeCRD)
			Expect(err).ToNot(HaveOccurred())

			// Verify old was deleted
			var deleted models.Scope
			err = DefaultContext.DB().Unscoped().Where("id = ?", oldID).First(&deleted).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted.DeletedAt).ToNot(BeNil())

			// and replaced by the new one
			var replacement models.Scope
			Expect(DefaultContext.DB().Where("id = ? AND deleted_at IS NULL", newID).First(&replacement).Error).To(Succeed())
		})

		ginkgo.It("replaces the old scope even when the new one is invalid", func() {
			oldID := uuid.New()
			targetsJSON, _ := json.Marshal([]v1.ScopeTarget{{Config: &types.ResourceSelector{Name: "old"}}})
			Expect(DefaultContext.DB().Create(&models.Scope{
				ID:        oldID,
				Name:      "replaced-scope",
				Namespace: "default",
				Targets:   types.JSON(targetsJSON),
				Source:    models.SourceCRD,
			}).Error).To(Succeed())

			newScopeCRD := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: "replaced-scope", Namespace: "default", UID: k8sTypes.UID(uuid.New().String())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &types.ResourceSelector{}}}},
			}
			Expect(DeleteStaleScope(DefaultContext, newScopeCRD)).ToNot(Succeed())

			var old models.Scope
			Expect(DefaultContext.DB().Unscoped().Where("id = ?", oldID).First(&old).Error).To(Succeed())
			Expect(old.DeletedAt).ToNot(BeNil(), "there's no previous version to fall back to")

			var replacement models.Scope
			Expect(DefaultContext.DB().Where("id = ? AND deleted_at IS NULL", newScopeCRD.UID).First(&replacement).Error).To(Succeed())
			Expect(replacement.Error).ToNot(BeNil())
		})
	})
})
