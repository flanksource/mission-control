package upstream

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/incident-commander/api"
	icrbac "github.com/flanksource/incident-commander/rbac"

	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	dutyUpstream "github.com/flanksource/duty/upstream"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = ginkgo.Describe("Upstream Push", ginkgo.Ordered, func() {
	var (
		pushAgent    = agentWrapper{name: "push", id: uuid.New(), datasetFunc: dummy.GenerateDynamicDummyData}
		pushUpstream = agentWrapper{name: "push_upstream", id: uuid.New()}
	)

	ginkgo.BeforeAll(func() {
		pushAgent.setup(DefaultContext)
		pushUpstream.setup(DefaultContext)
		pushUpstream.StartServer()

		var system models.Person
		Expect(pushUpstream.DB().Where("name = ?", "System").First(&system).Error).To(Succeed())
		Expect(icrbac.Init(DefaultContext)).To(Succeed())

		DefaultContext.ClearCache()
		properties.Global.Set("upstream.reconcile.pre-check", "false")

		// The agent authenticates as its own person, which the upstream resolves the agent from
		agentPerson := models.Person{ID: uuid.New(), Name: "push-agent", Type: "agent"}
		Expect(pushUpstream.DB().Create(&agentPerson).Error).To(BeNil())
		Expect(dutyRBAC.AddRoleForUser(agentPerson.ID.String(), policy.RoleAgent)).To(Succeed())
		Expect(pushUpstream.DB().Create(&models.Agent{ID: pushAgent.id, Name: pushAgent.name, PersonID: &agentPerson.ID}).Error).To(BeNil())
		pushAgent.username = agentPerson.Name
	})

	ginkgo.It("should push all tables", func() {
		err := pushAgent.Reconcile(pushUpstream.port)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.Describe("consumer", func() {
		ginkgo.It("should have transferred all the components", func() {
			var fieldsToIgnore []string
			fieldsToIgnore = append(fieldsToIgnore, "TopologyID")                                                    // Upstream creates its own dummy topology
			fieldsToIgnore = append(fieldsToIgnore, "Checks", "Components", "Order", "SelectorID", "RelationshipID") // These are auxiliary fields & do not represent the table columns.
			ignoreFieldsOpt := cmpopts.IgnoreFields(models.Component{}, fieldsToIgnore...)

			// unexported fields must be explicitly ignored.
			ignoreUnexportedOpt := cmpopts.IgnoreUnexported(models.Component{}, types.Summary{})

			compareAgentEntities[models.Component]("", pushUpstream.DB(), pushAgent, ignoreFieldsOpt, ignoreUnexportedOpt, cmpopts.IgnoreFields(models.Component{}, "AgentID"))
		})

		ginkgo.It("should have transferred all the checks", func() {
			compareAgentEntities[models.Check]("", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.Check{}, "AgentID"))
		})

		ginkgo.It("should have transferred all the check statuses", func() {
			compareAgentEntities[models.CheckStatus]("check_statuses", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.CheckStatus{}, "IsPushed"))
		})

		ginkgo.It("should have transferred all the canaries", func() {
			compareAgentEntities[models.Canary]("", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.Canary{}, "AgentID"))
		})

		ginkgo.It("should have transferred all the configs", func() {
			compareAgentEntities[models.ConfigItem]("", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.ConfigItem{}, "AgentID", "InsertedAt"))
		})

		ginkgo.It("should have transferred all the config relationships", func() {
			compareAgentEntities[models.ComponentRelationship]("component_relationships", pushUpstream.DB(), pushAgent)
		})

		ginkgo.It("should have transferred all the config component relationships", func() {
			compareAgentEntities[models.ConfigComponentRelationship]("config_component_relationships", pushUpstream.DB(), pushAgent)
		})

		ginkgo.It("should have populated the agent_id field", func() {
			var count int
			err := pushUpstream.DB().Select("COUNT(*)").Table("checks").Where("agent_id IS NOT NULL").Scan(&count).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(count).ToNot(BeZero())
		})
	})
})

var _ = ginkgo.Describe("Upstream artifact upload", ginkgo.Ordered, func() {
	var (
		artifactUpstream = agentWrapper{name: "artifact_upstream"}
		owner            = models.Agent{ID: uuid.New(), Name: "artifact-owner"}
		intruder         = models.Agent{ID: uuid.New(), Name: "artifact-intruder"}
		artifact         models.Artifact
		storeDir         string
		previousConn     string
	)

	upload := func(agent models.Agent, artifactID uuid.UUID, body string) int {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%d/upstream/artifacts/%s", artifactUpstream.port, artifactID), strings.NewReader(body))
		Expect(err).ToNot(HaveOccurred())
		req.SetBasicAuth(agent.Name, "admin")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		return resp.StatusCode
	}

	storedContent := func() string {
		content, err := os.ReadFile(filepath.Join(storeDir, artifact.Path))
		Expect(err).ToNot(HaveOccurred())
		return string(content)
	}

	ginkgo.BeforeAll(func() {
		artifactUpstream.setup(DefaultContext)
		artifactUpstream.StartServer()
		Expect(icrbac.Init(DefaultContext)).To(Succeed())
		db := artifactUpstream.DB()

		for _, agent := range []*models.Agent{&owner, &intruder} {
			person := models.Person{ID: uuid.New(), Name: agent.Name, Type: "agent"}
			Expect(db.Create(&person).Error).To(Succeed())
			Expect(dutyRBAC.AddRoleForUser(person.ID.String(), policy.RoleAgent)).To(Succeed())
			agent.PersonID = &person.ID
			Expect(db.Create(agent).Error).To(Succeed())
		}

		storeDir = ginkgo.GinkgoT().TempDir()
		Expect(db.Create(&models.Connection{
			Name:       "artifacts",
			Namespace:  "default",
			Source:     models.SourceUI,
			Type:       models.ConnectionTypeFolder,
			Properties: types.JSONStringMap{"path": storeDir},
		}).Error).To(Succeed())
		previousConn = api.DefaultArtifactConnection
		api.DefaultArtifactConnection = "connection://default/artifacts"

		canary := models.Canary{ID: uuid.New(), Name: "owner-canary", Namespace: "default", AgentID: owner.ID, Spec: []byte("{}")}
		Expect(db.Create(&canary).Error).To(Succeed())
		check := models.Check{ID: uuid.New(), CanaryID: canary.ID, AgentID: owner.ID, Name: "owner-check", Type: "http"}
		Expect(db.Create(&check).Error).To(Succeed())

		artifact = models.Artifact{ID: uuid.New(), CheckID: &check.ID, Filename: "out.txt"}
		artifact.Path = dutyUpstream.AgentArtifactPath(owner.ID, artifact.ID)
		Expect(db.Create(&artifact).Error).To(Succeed())

		Expect(upload(owner, artifact.ID, "A")).To(Equal(http.StatusOK))
		Expect(storedContent()).To(Equal("A"))
	})

	ginkgo.AfterAll(func() {
		api.DefaultArtifactConnection = previousConn
	})

	ginkgo.It("refuses an upload to another agent's artifact", func() {
		Expect(upload(intruder, artifact.ID, "B")).To(Equal(http.StatusForbidden))
		Expect(storedContent()).To(Equal("A"))
	})

	ginkgo.It("refuses an upload to an artifact that doesn't exist", func() {
		Expect(upload(intruder, uuid.New(), "B")).To(Equal(http.StatusNotFound))

		var written []string
		Expect(filepath.WalkDir(storeDir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				written = append(written, path)
			}
			return err
		})).To(Succeed())
		Expect(written).To(ConsistOf(filepath.Join(storeDir, artifact.Path)))
	})

	ginkgo.It("lets the owner replace its artifact's content", func() {
		Expect(upload(owner, artifact.ID, "A2")).To(Equal(http.StatusOK))
		Expect(storedContent()).To(Equal("A2"))
	})
})

// compareAgentEntities is a helper function that compares two sets of entities from an upstream and downstream database,
// ensuring that all records have been successfully transferred and match each other.
func compareAgentEntities[T any](table string, upstreamDB *gorm.DB, agent agentWrapper, ignoreOpts ...cmp.Option) {
	var upstream, downstream []T
	var err error
	var agentErr error

	// We're conditionally fetching the records from upstream and agent db
	// because we need to ensure
	// - that upstream only fetches the items for the given agent
	// - and the order of the items when fetching from upstream and agent db is identitcal for the comparison to work
	switch table {
	case "check_statuses":
		err = upstreamDB.Joins("LEFT JOIN checks ON checks.id = check_statuses.check_id").Where("checks.agent_id = ?", agent.id).Order("check_id, time").Find(&upstream).Error
		agentErr = agent.DB().Order("check_id, time").Find(&downstream).Error

	case "component_relationships":
		err = upstreamDB.Joins("LEFT JOIN components c1 ON component_relationships.component_id = c1.id").
			Joins("LEFT JOIN components c2 ON component_relationships.relationship_id = c2.id").
			Where("c1.agent_id = ? OR c2.agent_id = ?", agent.id, agent.id).Order("created_at").Find(&upstream).Error
		agentErr = agent.DB().Order("created_at").Find(&downstream).Error

	case "config_component_relationships":
		err = upstreamDB.Joins("LEFT JOIN components ON config_component_relationships.component_id = components.id").
			Joins("LEFT JOIN config_items ON config_items.id = config_component_relationships.config_id").
			Where("components.agent_id = ? OR config_items.agent_id = ?", agent.id, agent.id).Order("component_id, config_id, created_at").Find(&upstream).Error
		agentErr = agent.DB().Order("component_id, config_id, created_at").Find(&downstream).Error

	default:
		err = upstreamDB.Where("agent_id = ?", agent.id).Order("id").Find(&upstream).Error
		agentErr = agent.DB().Order("id").Find(&downstream).Error
	}

	Expect(err).NotTo(HaveOccurred())
	Expect(agentErr).NotTo(HaveOccurred())

	Expect(len(upstream)).To(Equal(len(downstream)), fmt.Sprintf("expected %s to sync all items to upstream ", agent.name))

	ignoreOpts = append(ignoreOpts, cmpopts.IgnoreUnexported(models.ConfigItem{}))
	diff := cmp.Diff(upstream, downstream, ignoreOpts...)
	Expect(diff).To(BeEmpty(), fmt.Sprintf("expected %s to sync correct items to upstream ", agent.name))
}
