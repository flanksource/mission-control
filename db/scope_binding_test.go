package db

import (
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("ScopeBinding Persistence", func() {
	ginkgo.Context("PersistScopeBindingFromCRD", func() {
		ginkgo.It("should persist a valid ScopeBinding CRD", func() {
			bindingObj := &v1.ScopeBinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "mission-control.flanksource.com/v1",
					Kind:       "ScopeBinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-binding",
					Namespace: "default",
					UID:       k8sTypes.UID(uuid.New().String()),
				},
				Spec: v1.ScopeBindingSpec{
					Description: "Test binding",
					Subjects: v1.ScopeBindingSubjects{
						Persons: []string{"aditya@flanksource.com", "moshe@flanksource.com"},
						Teams:   []string{"platform-team"},
					},
					Scopes: []string{"prod-configs", "staging-components"},
				},
			}

			err := PersistScopeBindingFromCRD(DefaultContext, bindingObj)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was saved
			var saved models.ScopeBinding
			err = DefaultContext.DB().Where("id = ?", bindingObj.UID).First(&saved).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(saved.Name).To(Equal("test-binding"))
			Expect(saved.Namespace).To(Equal("default"))
			Expect(saved.Description).To(Equal("Test binding"))
			Expect(saved.Persons).To(Equal(pq.StringArray{"aditya@flanksource.com", "moshe@flanksource.com"}))
			Expect(saved.Teams).To(Equal(pq.StringArray{"platform-team"}))
			Expect(saved.Scopes).To(Equal(pq.StringArray{"prod-configs", "staging-components"}))
		})

		ginkgo.It("should fail with empty subjects", func() {
			bindingObj := &v1.ScopeBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-subjects",
					Namespace: "default",
					UID:       k8sTypes.UID(uuid.New().String()),
				},
				Spec: v1.ScopeBindingSpec{
					Subjects: v1.ScopeBindingSubjects{}, // Empty
					Scopes:   []string{"some-scope"},
				},
			}

			err := PersistScopeBindingFromCRD(DefaultContext, bindingObj)
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("should fail with invalid UID", func() {
			bindingObj := &v1.ScopeBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bad-uid",
					Namespace: "default",
					UID:       "not-a-valid-uuid",
				},
				Spec: v1.ScopeBindingSpec{
					Subjects: v1.ScopeBindingSubjects{
						Persons: []string{"test@example.com"},
					},
					Scopes: []string{"some-scope"},
				},
			}

			err := PersistScopeBindingFromCRD(DefaultContext, bindingObj)
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Context("DeleteScopeBinding", func() {
		ginkgo.It("should soft delete a ScopeBinding", func() {
			// Create a binding first
			bindingID := uuid.New()
			binding := models.ScopeBinding{
				ID:        bindingID,
				Name:      "delete-me",
				Namespace: "default",
				Source:    models.SourceCRD,
				Persons:   pq.StringArray{"test@example.com"},
				Teams:     pq.StringArray{},
				Scopes:    pq.StringArray{"test-scope"},
			}
			err := DefaultContext.DB().Create(&binding).Error
			Expect(err).ToNot(HaveOccurred())

			// Delete it
			err = DeleteScopeBinding(DefaultContext, bindingID.String())
			Expect(err).ToNot(HaveOccurred())

			// Verify soft delete
			var deleted models.ScopeBinding
			err = DefaultContext.DB().Unscoped().Where("id = ?", bindingID).First(&deleted).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted.DeletedAt).ToNot(BeNil())
		})
	})

	ginkgo.Context("DeleteStaleScopeBinding", func() {
		ginkgo.It("should delete old bindings with same name/namespace", func() {
			oldID := uuid.New()
			newID := uuid.New()

			// Create old binding
			oldBinding := models.ScopeBinding{
				ID:        oldID,
				Name:      "my-binding",
				Namespace: "default",
				Source:    models.SourceCRD,
				Persons:   pq.StringArray{"test@example.com"},
				Teams:     pq.StringArray{},
				Scopes:    pq.StringArray{"test-scope"},
			}
			err := DefaultContext.DB().Create(&oldBinding).Error
			Expect(err).ToNot(HaveOccurred())

			// Create new binding CRD
			newBindingCRD := &v1.ScopeBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-binding",
					Namespace: "default",
					UID:       k8sTypes.UID(newID.String()),
				},
			}

			// Delete stale
			err = DeleteStaleScopeBinding(DefaultContext, newBindingCRD)
			Expect(err).ToNot(HaveOccurred())

			// Verify old was deleted
			var deleted models.ScopeBinding
			err = DefaultContext.DB().Unscoped().Where("id = ?", oldID).First(&deleted).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted.DeletedAt).ToNot(BeNil())
		})
	})
})
