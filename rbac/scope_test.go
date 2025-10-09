package rbac

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("Scope RLS Integration", func() {
	ginkgo.Context("GetScopeBindingsForPerson", func() {
		ginkgo.It("should retrieve bindings by person email", func() {
			person := dummy.JohnDoe

			binding := models.ScopeBinding{
				ID:        uuid.New(),
				Name:      "test-binding",
				Namespace: "default",
				Source:    models.SourceCRD,
				Persons:   pq.StringArray{person.Email},
				Teams:     pq.StringArray{},
				Scopes:    pq.StringArray{"test-scope"},
			}
			err := DefaultContext.DB().Create(&binding).Error
			Expect(err).ToNot(HaveOccurred())

			bindings, err := GetScopeBindingsForPerson(DefaultContext, person.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(bindings)).To(BeNumerically(">=", 1))

			// Find our binding in the results
			found := false
			for _, b := range bindings {
				if b.ID == binding.ID {
					found = true
					Expect(b.Name).To(Equal("test-binding"))
					break
				}
			}
			Expect(found).To(BeTrue())
		})

		// TODO: Add team membership test once team_members table is properly set up in test fixtures
	})

	ginkgo.Context("GetScopesForPerson", func() {
		ginkgo.It("should retrieve scopes referenced by bindings", func() {
			person := dummy.JohnDoe

			// Create scope
			targetsJSON, _ := json.Marshal([]v1.ScopeTarget{
				{
					Config: &v1.ScopeResourceSelector{
						Name:        "prod-*",
						TagSelector: "env=prod",
					},
				},
			})

			scope := models.Scope{
				ID:        uuid.New(),
				Name:      "test-prod-configs",
				Namespace: "default",
				Source:    models.SourceCRD,
				Targets:   types.JSON(targetsJSON),
			}
			err := DefaultContext.DB().Create(&scope).Error
			Expect(err).ToNot(HaveOccurred())

			// Create binding
			binding := models.ScopeBinding{
				ID:        uuid.New(),
				Name:      "user-binding",
				Namespace: "default",
				Source:    models.SourceCRD,
				Persons:   pq.StringArray{person.Email},
				Teams:     pq.StringArray{},
				Scopes:    pq.StringArray{"test-prod-configs"},
			}
			err = DefaultContext.DB().Create(&binding).Error
			Expect(err).ToNot(HaveOccurred())

			scopes, err := GetScopesForPerson(DefaultContext, person.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(scopes)).To(BeNumerically(">=", 1))

			// Find our scope in the results
			found := false
			for _, s := range scopes {
				if s.ID == scope.ID {
					found = true
					Expect(s.Name).To(Equal("test-prod-configs"))
					break
				}
			}
			Expect(found).To(BeTrue())
		})
	})

	ginkgo.Context("ExtractTargetsFromScopes", func() {
		ginkgo.It("should extract targets with resource types", func() {
			targetsJSON, _ := json.Marshal([]v1.ScopeTarget{
				{
					Config: &v1.ScopeResourceSelector{
						Name:        "prod-*",
						Agent:       "agent1",
						TagSelector: "env=prod",
					},
				},
				{
					Component: &v1.ScopeResourceSelector{
						Name:        "staging-*",
						TagSelector: "env=staging",
					},
				},
			})

			scope := models.Scope{
				ID:        uuid.New(),
				Name:      "multi-target",
				Namespace: "default",
				Targets:   types.JSON(targetsJSON),
			}

			targets, err := ExtractTargetsFromScopes([]models.Scope{scope})
			Expect(err).ToNot(HaveOccurred())
			Expect(targets).To(HaveLen(2))

			// First target
			Expect(targets[0].ResourceType).To(Equal(v1.ScopeResourceConfig))
			Expect(targets[0].Selector.Name).To(Equal("prod-*"))
			Expect(targets[0].Selector.Agent).To(Equal("agent1"))
			Expect(targets[0].Selector.TagSelector).To(Equal("env=prod"))

			// Second target
			Expect(targets[1].ResourceType).To(Equal(v1.ScopeResourceComponent))
			Expect(targets[1].Selector.Name).To(Equal("staging-*"))
			Expect(targets[1].Selector.TagSelector).To(Equal("env=staging"))
		})
	})

	ginkgo.Context("parseTagSelector", func() {
		ginkgo.It("should parse tag selector string to map", func() {
			tags := parseTagSelector("env=prod,region=us-west")
			Expect(tags).To(HaveLen(2))
			Expect(tags["env"]).To(Equal("prod"))
			Expect(tags["region"]).To(Equal("us-west"))
		})

		ginkgo.It("should handle empty selector", func() {
			tags := parseTagSelector("")
			Expect(tags).To(BeNil())
		})

		ginkgo.It("should handle selector with spaces", func() {
			tags := parseTagSelector(" env = prod , region = us-west ")
			Expect(tags).To(HaveLen(2))
			Expect(tags["env"]).To(Equal("prod"))
			Expect(tags["region"]).To(Equal("us-west"))
		})
	})

	ginkgo.Context("GetRLSPayloadFromScopes", func() {
		ginkgo.It("should generate RLS payload for person with scopes", func() {
			person := dummy.JohnDoe

			// Create scope for configs
			configTargetsJSON, _ := json.Marshal([]v1.ScopeTarget{
				{
					Config: &v1.ScopeResourceSelector{
						Name:        "prod-*",
						TagSelector: "env=prod",
					},
				},
			})

			configScope := models.Scope{
				ID:        uuid.New(),
				Name:      "rls-prod-configs",
				Namespace: "default",
				Source:    models.SourceCRD,
				Targets:   types.JSON(configTargetsJSON),
			}
			err := DefaultContext.DB().Create(&configScope).Error
			Expect(err).ToNot(HaveOccurred())

			// Create scope for components
			componentTargetsJSON, _ := json.Marshal([]v1.ScopeTarget{
				{
					Component: &v1.ScopeResourceSelector{
						Name:        "staging-*",
						TagSelector: "env=staging",
					},
				},
			})

			componentScope := models.Scope{
				ID:        uuid.New(),
				Name:      "rls-staging-components",
				Namespace: "default",
				Source:    models.SourceCRD,
				Targets:   types.JSON(componentTargetsJSON),
			}
			err = DefaultContext.DB().Create(&componentScope).Error
			Expect(err).ToNot(HaveOccurred())

			// Create binding
			binding := models.ScopeBinding{
				ID:        uuid.New(),
				Name:      "test-access",
				Namespace: "default",
				Source:    models.SourceCRD,
				Persons:   pq.StringArray{person.Email},
				Teams:     pq.StringArray{},
				Scopes:    pq.StringArray{"rls-prod-configs", "rls-staging-components"},
			}
			err = DefaultContext.DB().Create(&binding).Error
			Expect(err).ToNot(HaveOccurred())

			// Generate RLS payload
			payload, err := GetRLSPayloadFromScopes(DefaultContext, person.ID)
			Expect(err).ToNot(HaveOccurred())

			// Verify payload structure
			Expect(len(payload.Config)).To(BeNumerically(">=", 1))
			Expect(len(payload.Component)).To(BeNumerically(">=", 1))

			// Find our specific scopes in the payload
			foundConfigScope := false
			for _, scope := range payload.Config {
				if len(scope.Names) > 0 && scope.Names[0] == "prod-*" {
					foundConfigScope = true
					Expect(scope.Tags).To(HaveKeyWithValue("env", "prod"))
					break
				}
			}
			Expect(foundConfigScope).To(BeTrue())

			foundComponentScope := false
			for _, scope := range payload.Component {
				if len(scope.Names) > 0 && scope.Names[0] == "staging-*" {
					foundComponentScope = true
					Expect(scope.Tags).To(HaveKeyWithValue("env", "staging"))
					break
				}
			}
			Expect(foundComponentScope).To(BeTrue())
		})
	})
})
