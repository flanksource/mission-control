package db

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("AccessScope Persistence", func() {
	// Use existing dummy fixtures that are created in BeforeSuite
	person := dummy.JohnDoe
	team := dummy.BackendTeam

	ginkgo.Context("Persisting AccessScope", func() {
		ginkgo.It("should persist AccessScope with person subject", func() {
			accessScope := &v1.AccessScope{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "mission-control.flanksource.com/v1",
					Kind:       "AccessScope",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-scope",
					Namespace: "default",
					UID:       k8sTypes.UID(uuid.New().String()),
				},
				Spec: v1.AccessScopeSpec{
					Subject: v1.AccessScopeSubject{
						Person: person.Email,
					},
					Resources: []v1.AccessScopeResourceType{v1.AccessScopeResourceConfig, v1.AccessScopeResourceComponent},
					Scopes: []v1.AccessScopeScope{
						{
							Tags: map[string]string{"namespace": "test"},
						},
					},
				},
			}

			err := PersistAccessScopeFromCRD(DefaultContext, accessScope)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was saved
			var saved models.AccessScope
			err = DefaultContext.DB().Where("name = ? AND namespace = ?", "test-scope", "default").First(&saved).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(saved.PersonID).ToNot(BeNil())
			Expect(*saved.PersonID).To(Equal(person.ID))
			Expect(saved.TeamID).To(BeNil())
			Expect(saved.Resources).To(Equal(pq.StringArray{"config", "component"}))

			// Verify scopes JSONB contains the expected data
			var scopes []v1.AccessScopeScope
			err = json.Unmarshal(saved.Scopes, &scopes)
			Expect(err).ToNot(HaveOccurred())
			Expect(scopes).To(HaveLen(1))
			Expect(scopes[0].Tags).To(HaveKeyWithValue("namespace", "test"))
		})

		ginkgo.It("should persist AccessScope with team subject", func() {
			accessScope := &v1.AccessScope{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "mission-control.flanksource.com/v1",
					Kind:       "AccessScope",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "team-scope",
					Namespace: "default",
					UID:       k8sTypes.UID(uuid.New().String()),
				},
				Spec: v1.AccessScopeSpec{
					Subject: v1.AccessScopeSubject{
						Team: team.Name,
					},
					Resources: []v1.AccessScopeResourceType{v1.AccessScopeResourceAll},
					Scopes: []v1.AccessScopeScope{
						{
							Agents: []string{"agent-1", "agent-2"},
						},
					},
				},
			}

			err := PersistAccessScopeFromCRD(DefaultContext, accessScope)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was saved
			var saved models.AccessScope
			err = DefaultContext.DB().Where("name = ? AND namespace = ?", "team-scope", "default").First(&saved).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(saved.TeamID).ToNot(BeNil())
			Expect(*saved.TeamID).To(Equal(team.ID))
			Expect(saved.PersonID).To(BeNil())

			// Verify scopes JSONB contains the expected data
			var scopes []v1.AccessScopeScope
			err = json.Unmarshal(saved.Scopes, &scopes)
			Expect(err).ToNot(HaveOccurred())
			Expect(scopes).To(HaveLen(1))
			Expect(scopes[0].Agents).To(ConsistOf("agent-1", "agent-2"))
		})

		ginkgo.It("should fail with invalid subject", func() {
			accessScope := &v1.AccessScope{
				ObjectMeta: metav1.ObjectMeta{
					UID: k8sTypes.UID(uuid.New().String()),
				},
				Spec: v1.AccessScopeSpec{
					Subject: v1.AccessScopeSubject{
						Person: "nonexistent@example.com",
					},
					Resources: []v1.AccessScopeResourceType{v1.AccessScopeResourceConfig},
					Scopes: []v1.AccessScopeScope{
						{
							Tags: map[string]string{"env": "dev"},
						},
					},
				},
			}

			err := PersistAccessScopeFromCRD(DefaultContext, accessScope)
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Context("Deleting AccessScope", func() {
		ginkgo.It("should soft delete AccessScope", func() {
			// Create AccessScope first
			scopesJSON, _ := json.Marshal([]v1.AccessScopeScope{
				{Tags: map[string]string{"env": "test"}},
			})
			accessScope := models.AccessScope{
				ID:        uuid.New(),
				Name:      "delete-test",
				Namespace: "default",
				PersonID:  &person.ID,
				Resources: pq.StringArray{"config"},
				Scopes:    types.JSON(scopesJSON),
				Source:    models.SourceCRD,
			}
			Expect(DefaultContext.DB().Create(&accessScope).Error).ToNot(HaveOccurred())

			// Delete it
			err := DeleteAccessScope(DefaultContext, accessScope.ID.String())
			Expect(err).ToNot(HaveOccurred())

			// Verify it's soft deleted
			var deleted models.AccessScope
			err = DefaultContext.DB().Unscoped().Where("id = ?", accessScope.ID).First(&deleted).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted.DeletedAt).ToNot(BeNil())
		})

		ginkgo.It("should delete stale AccessScope with same name/namespace", func() {
			// Create first AccessScope
			scopesJSON, _ := json.Marshal([]v1.AccessScopeScope{
				{Tags: map[string]string{"env": "test"}},
			})
			oldAccessScope := models.AccessScope{
				ID:        uuid.New(),
				Name:      "stale-test",
				Namespace: "default",
				PersonID:  &person.ID,
				Resources: pq.StringArray{"config"},
				Scopes:    types.JSON(scopesJSON),
				Source:    models.SourceCRD,
			}
			Expect(DefaultContext.DB().Create(&oldAccessScope).Error).ToNot(HaveOccurred())

			// Create newer AccessScope with same name/namespace but different ID
			newAccessScope := &v1.AccessScope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stale-test",
					Namespace: "default",
					UID:       k8sTypes.UID(uuid.New().String()),
				},
				Spec: v1.AccessScopeSpec{
					Subject: v1.AccessScopeSubject{
						Person: person.Email,
					},
					Resources: []v1.AccessScopeResourceType{v1.AccessScopeResourceConfig},
					Scopes: []v1.AccessScopeScope{
						{Tags: map[string]string{"env": "prod"}},
					},
				},
			}

			// Delete stale AccessScope
			err := DeleteStaleAccessScope(DefaultContext, newAccessScope)
			Expect(err).ToNot(HaveOccurred())

			// Verify old one is deleted
			var deleted models.AccessScope
			err = DefaultContext.DB().Unscoped().Where("id = ?", oldAccessScope.ID).First(&deleted).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted.DeletedAt).ToNot(BeNil())
		})
	})
})
