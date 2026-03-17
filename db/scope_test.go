package db

import (
	"encoding/json"

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
							Config: &v1.ScopeResourceSelector{
								Name:        "prod-*",
								Agent:       "homelab",
								TagSelector: "env=prod",
							},
						},
					},
				},
			}

			err := PersistScopeFromCRD(DefaultContext, scopeObj)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was saved
			var saved models.Scope
			err = DefaultContext.DB().Where("id = ?", scopeObj.UID).First(&saved).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(saved.Name).To(Equal("test-scope"))
			Expect(saved.Namespace).To(Equal("default"))
			Expect(saved.Description).To(Equal("Test scope"))

			// Verify targets JSON
			var targets []v1.ScopeTarget
			err = json.Unmarshal(saved.Targets, &targets)
			Expect(err).ToNot(HaveOccurred())
			Expect(targets).To(HaveLen(1))
			Expect(targets[0].Config).ToNot(BeNil())
			Expect(targets[0].Config.Name).To(Equal("prod-*"))
			Expect(targets[0].Config.Agent).To(Equal(dummy.HomelabAgent.ID.String()))
			Expect(targets[0].Config.TagSelector).To(Equal("env=prod"))
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
							Config: &v1.ScopeResourceSelector{Name: "test"},
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
					Config: &v1.ScopeResourceSelector{Name: "test"},
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
					Config: &v1.ScopeResourceSelector{Name: "old"},
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
			}

			// Delete stale
			err = DeleteStaleScope(DefaultContext, newScopeCRD)
			Expect(err).ToNot(HaveOccurred())

			// Verify old was deleted
			var deleted models.Scope
			err = DefaultContext.DB().Unscoped().Where("id = ?", oldID).First(&deleted).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted.DeletedAt).ToNot(BeNil())
		})
	})
})
