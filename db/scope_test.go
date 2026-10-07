package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
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
							Config: &v1.ScopeConfigSelector{
								ScopeResourceRef: v1.ScopeResourceRef{Name: "prod"},
								Agent:            "homelab",
								TagSelector:      "env=prod",
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
			{"an empty selector", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{}}},
			{"a name suffix", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*-db"}}}},
			{"a wildcard in the middle of a name", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "prod-*-db"}}}},
			{"a tag exclusion", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "env!=prod"}}},
			{"a bare label key", v1.ScopeTarget{Component: &v1.ScopeLabelledSelector{LabelSelector: "team"}}},
			{"a malformed tagSelector", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "env in (prod"}}},
			{"two resource types in one target", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}, Playbook: &v1.ScopePlaybookRef{Name: "*"}}},
			{"a wildcard namespace", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Namespace: "*"}}}},
			{"an agent that doesn't exist", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{Agent: "no-such-agent"}}},
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

		ginkgo.Context("membership", func() {
			members := func(id uuid.UUID) []uuid.UUID {
				var ids []uuid.UUID
				Expect(DefaultContext.DB().Raw("SELECT resource_id FROM scope_members WHERE scope_id = ? AND resource_id IS NOT NULL", id).Scan(&ids).Error).To(Succeed())
				return ids
			}

			eksScope := func(name string) *v1.Scope {
				return &v1.Scope{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: k8sTypes.UID(uuid.New().String())},
					Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: *dummy.EKSCluster.Name}}}}},
				}
			}

			ginkgo.It("stores the membership of a valid scope, and clears it when the scope becomes invalid or is deleted", func() {
				scopeObj := eksScope("membership-scope")
				Expect(PersistScopeFromCRD(DefaultContext, scopeObj)).To(Succeed())
				id := uuid.MustParse(string(scopeObj.UID))
				Expect(members(id)).To(ContainElement(dummy.EKSCluster.ID))

				scopeObj.Spec.Targets = []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{Agent: "no-such-agent"}}}
				Expect(PersistScopeFromCRD(DefaultContext, scopeObj)).ToNot(Succeed())
				built, err := membership.IsBuilt(DefaultContext, id)
				Expect(err).ToNot(HaveOccurred())
				Expect(built).To(BeFalse(), "an invalid scope has no membership")

				scopeObj.Spec.Targets = eksScope("").Spec.Targets
				Expect(PersistScopeFromCRD(DefaultContext, scopeObj)).To(Succeed())
				Expect(members(id)).To(ContainElement(dummy.EKSCluster.ID))

				Expect(DeleteScope(DefaultContext, id.String())).To(Succeed())
				built, err = membership.IsBuilt(DefaultContext, id)
				Expect(err).ToNot(HaveOccurred())
				Expect(built).To(BeFalse(), "a deleted scope has no membership")
			})

			ginkgo.It("builds, at startup, the membership of a scope stored before membership was", func() {
				scopeObj := eksScope("unbuilt-scope")
				targets, err := json.Marshal(scopeObj.Spec.Targets)
				Expect(err).ToNot(HaveOccurred())
				scope := models.Scope{ID: uuid.MustParse(string(scopeObj.UID)), Name: scopeObj.Name, Namespace: scopeObj.Namespace, Targets: types.JSON(targets), Source: models.SourceCRD}
				Expect(DefaultContext.DB().Create(&scope).Error).To(Succeed())
				ginkgo.DeferCleanup(func() { Expect(DeleteScope(DefaultContext, scope.ID.String())).To(Succeed()) })
				Expect(members(scope.ID)).To(BeEmpty())

				Expect(adapter.BuildScopeMemberships(DefaultContext)).To(Succeed())
				Expect(members(scope.ID)).To(ContainElement(dummy.EKSCluster.ID))
			})

			ginkgo.It("fails the save, keeping the previous version, when the membership can't be rebuilt", func() {
				timeout, retries := membership.LockTimeout, membership.LockRetries
				membership.LockTimeout, membership.LockRetries = 100*time.Millisecond, 0
				ginkgo.DeferCleanup(func() { membership.LockTimeout, membership.LockRetries = timeout, retries })

				scopeObj := eksScope("locked-scope")
				Expect(PersistScopeFromCRD(DefaultContext, scopeObj)).To(Succeed())
				ginkgo.DeferCleanup(func() { Expect(DeleteScope(DefaultContext, string(scopeObj.UID))).To(Succeed()) })

				writer := DefaultContext.DB().Begin()
				Expect(writer.Exec("SELECT pg_advisory_xact_lock_shared(hashtext('scope_membership'))").Error).To(Succeed())

				changed := scopeObj.DeepCopy()
				changed.Spec.Targets = []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "staging-db"}}}}
				Expect(PersistScopeFromCRD(DefaultContext, changed)).To(MatchError(ContainSubstring("lock")))
				Expect(writer.Rollback().Error).To(Succeed())

				var saved models.Scope
				Expect(DefaultContext.DB().Where("id = ?", scopeObj.UID).First(&saved).Error).To(Succeed())
				Expect(string(saved.Targets)).To(ContainSubstring(*dummy.EKSCluster.Name), "the previous version stays")
				Expect(members(saved.ID)).To(ContainElement(dummy.EKSCluster.ID))
			})
		})

		ginkgo.It("records why a scope is invalid", func() {
			scopeObj := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: "missing-agent", Namespace: "default", UID: k8sTypes.UID(uuid.New().String())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{Agent: "no-such-agent"}}}},
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
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{Agent: dummy.HomelabAgent.ID.String()}}}},
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
							Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "test"}},
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
					Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "test"}},
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

	ginkgo.Context("ValidateScope", func() {
		ginkgo.It("resolves agents on every supported target without changing the authored scope", func() {
			agents := []models.Agent{dummy.GCPAgent, dummy.HomelabAgent, dummy.GCPAgent, dummy.HomelabAgent}
			var targets []v1.ScopeTarget
			for i, kind := range []string{"config", "component", "check", "canary"} {
				var target v1.ScopeTarget
				Expect(json.Unmarshal([]byte(fmt.Sprintf(`{"%s":{"agent":%q}}`, kind, agents[i].Name)), &target)).To(Succeed())
				targets = append(targets, target)
			}

			resolved, err := adapter.ValidateScope(DefaultContext, targets)
			Expect(err).ToNot(HaveOccurred())
			for i, target := range resolved {
				_, selector := target.Selector()
				Expect(selector.Agent).To(Equal(agents[i].ID.String()))
				_, original := targets[i].Selector()
				Expect(original.Agent).To(Equal(agents[i].Name))
			}
		})
	})

	ginkgo.Context("DeleteStaleScope", func() {
		ginkgo.It("should delete old scopes with same name/namespace", func() {
			oldID := uuid.New()
			newID := uuid.New()

			// Create old scope
			targetsJSON, _ := json.Marshal([]v1.ScopeTarget{
				{
					Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "old"}},
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
				Spec: v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "new"}}}}},
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

		ginkgo.It("clears the old scope's membership and builds the new one's", func() {
			byName := func(uid uuid.UUID, name string) *v1.Scope {
				return &v1.Scope{
					ObjectMeta: metav1.ObjectMeta{Name: "rebuilt-scope", Namespace: "default", UID: k8sTypes.UID(uid.String())},
					Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: name}}}}},
				}
			}
			members := func(id uuid.UUID) []uuid.UUID {
				var ids []uuid.UUID
				Expect(DefaultContext.DB().Raw("SELECT resource_id FROM scope_members WHERE scope_id = ?", id).Scan(&ids).Error).To(Succeed())
				return ids
			}

			oldID, newID := uuid.New(), uuid.New()
			Expect(PersistScopeFromCRD(DefaultContext, byName(oldID, *dummy.EKSCluster.Name))).To(Succeed())
			Expect(members(oldID)).To(ContainElement(dummy.EKSCluster.ID))

			Expect(DeleteStaleScope(DefaultContext, byName(newID, *dummy.KubernetesCluster.Name))).To(Succeed())
			ginkgo.DeferCleanup(func() { Expect(DeleteScope(DefaultContext, newID.String())).To(Succeed()) })

			Expect(members(oldID)).To(BeEmpty())
			Expect(members(newID)).To(ContainElement(dummy.KubernetesCluster.ID))
		})

		ginkgo.It("replaces the old scope even when the new one is invalid", func() {
			oldID := uuid.New()
			targetsJSON, _ := json.Marshal([]v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "old"}}}})
			Expect(DefaultContext.DB().Create(&models.Scope{
				ID:        oldID,
				Name:      "replaced-scope",
				Namespace: "default",
				Targets:   types.JSON(targetsJSON),
				Source:    models.SourceCRD,
			}).Error).To(Succeed())

			newScopeCRD := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: "replaced-scope", Namespace: "default", UID: k8sTypes.UID(uuid.New().String())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{}}}},
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
