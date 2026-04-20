package playbook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/upstream"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"github.com/samber/oops"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook/sdk"
	"github.com/flanksource/incident-commander/playbook/testdata"
)

var _ = Describe("Playbook", Ordered, func() {
	var _ = Describe("Artifacts", Ordered, func() {
		It("run exec action and save artifacts", func() {
			run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "action-exec-artifacts", RunParams{
				ConfigID: lo.ToPtr(dummy.EKSCluster.ID),
			}, models.PlaybookRunStatusCompleted)

			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).To(BeNil())

			Expect(actions).To(HaveLen(1))
			Expect(actions[0].Status).To(Equal(models.PlaybookActionStatusCompleted))

			var artifacts []models.Artifact
			err = DefaultContext.DB().Where("playbook_run_action_id = ?", actions[0].ID).Find(&artifacts).Error
			Expect(err).To(BeNil())
			Expect(artifacts).To(HaveLen(1))
			Expect(artifacts[0].Filename).To(Equal("stdout"))
			Expect(artifacts[0].Size).To(BeNumerically(">", 0))
		})
	})

	var _ = Describe("AI", Ordered, Label("ignore_local"), func() {
		Skip("API Key exhaused. TODO: Fix token")
		Context("should run AI action and save artifacts", func() {
			var actions []models.PlaybookRunAction
			var artifactList []models.Artifact

			It("should have run the action", func() {
				run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "action-ai", RunParams{
					ConfigID: lo.ToPtr(dummy.KubernetesNodeAKSPool1.ID),
				}, models.PlaybookRunStatusCompleted)

				err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
				Expect(err).To(BeNil())

				Expect(actions).To(HaveLen(1))
				Expect(actions[0].Status).To(Equal(models.PlaybookActionStatusCompleted))

				err = DefaultContext.DB().Where("playbook_run_action_id = ?", actions[0].ID).Find(&artifactList).Error
				Expect(err).To(BeNil())
				Expect(artifactList).To(HaveLen(1))
			})

			It("should have saved the prompt", func() {
				Expect(artifactList[0].Filename).To(Equal("prompt.md"))
				Expect(artifactList[0].Size).To(BeNumerically(">", 0))

				conn, err := DefaultContext.HydrateConnectionByURL(api.DefaultArtifactConnection)
				Expect(err).To(BeNil())
				Expect(conn).ToNot(BeNil())

				artifactStore, err := artifacts.GetFSForConnection(DefaultContext, *conn)
				Expect(err).To(BeNil())

				prompt, err := artifactStore.Read(DefaultContext, artifactList[0].Path)
				Expect(err).To(BeNil())

				content, err := io.ReadAll(prompt)
				Expect(err).To(BeNil())

				Expect(string(content)).To(ContainSubstring("Find out why %s is not healthy", *dummy.KubernetesNodeAKSPool1.Name))
			})

			It("should have generated valid json result", func() {
				Expect(actions[0].Result).ToNot(BeNil())
				Expect(actions[0].Result).To(HaveKey("json"))
				Expect(actions[0].Result).To(HaveKey("generationInfo"))

				var jsonResult map[string]any
				err := json.Unmarshal([]byte(actions[0].Result["json"].(string)), &jsonResult)
				Expect(err).To(BeNil())

				Expect(jsonResult).To(HaveKey("headline"))
				Expect(jsonResult).To(HaveKey("summary"))
				Expect(jsonResult).To(HaveKey("recommended_fix"))
			})
		})
	})

	var _ = Describe("AI Catalog", Ordered, Label("ignore_local"), func() {
		Skip("API Key exhaused. TODO: Fix token")
		var testConfigID uuid.UUID

		BeforeAll(func() {
			// Create a config item representing an IAM policy
			testConfigID = uuid.New()
			configItem := models.ConfigItem{
				ID:          testConfigID,
				Name:        lo.ToPtr("production-admin-policy"),
				ConfigClass: "IAMPolicy",
				Type:        lo.ToPtr("AWS::IAM::Policy"),
				Config:      lo.ToPtr(`{"PolicyName":"AdminAccess","Effect":"Allow","Action":"*","Resource":"*"}`),
				// cluster=aws tag is required to match the RBAC permission in allow-playbook.yaml
				// that grants JohnDoe playbook:run access on configs with tagSelector cluster=aws
				Tags: types.JSONStringMap{"cluster": "aws"},
			}
			err := DefaultContext.DB().Create(&configItem).Error
			Expect(err).To(BeNil())

			// Create config changes with varying severities simulating security-related access events
			now := time.Now()
			changes := []models.ConfigChange{
				{
					ConfigID:   testConfigID.String(),
					ChangeType: "PolicyAttached",
					Severity:   models.SeverityCritical,
					Source:     "Cloudtrail",
					Summary:    "Wildcard admin policy attached to new IAM role developer-temp",
					Details:    types.JSON(`{"principal":"arn:aws:iam::123456:role/developer-temp","action":"iam:AttachRolePolicy"}`),
					CreatedAt:  lo.ToPtr(now.Add(-time.Hour * 2)),
					Count:      1,
				},
				{
					ConfigID:   testConfigID.String(),
					ChangeType: "PermissionEscalation",
					Severity:   models.SeverityHigh,
					Source:     "Cloudtrail",
					Summary:    "IAM user escalated own permissions by attaching AdministratorAccess policy",
					Details:    types.JSON(`{"principal":"arn:aws:iam::123456:user/jsmith","action":"iam:AttachUserPolicy"}`),
					CreatedAt:  lo.ToPtr(now.Add(-time.Hour * 6)),
					Count:      1,
				},
				{
					ConfigID:   testConfigID.String(),
					ChangeType: "UnusedAccess",
					Severity:   models.SeverityMedium,
					Source:     "AccessAnalyzer",
					Summary:    "Service role has not been used in 90 days but retains admin privileges",
					Details:    types.JSON(`{"principal":"arn:aws:iam::123456:role/legacy-service","lastUsed":"2025-01-01T00:00:00Z"}`),
					CreatedAt:  lo.ToPtr(now.Add(-time.Hour * 12)),
					Count:      1,
				},
				{
					ConfigID:   testConfigID.String(),
					ChangeType: "MFADisabled",
					Severity:   models.SeverityHigh,
					Source:     "Cloudtrail",
					Summary:    "MFA device deactivated for root account",
					Details:    types.JSON(`{"principal":"arn:aws:iam::123456:root","action":"iam:DeactivateMFADevice"}`),
					CreatedAt:  lo.ToPtr(now.Add(-time.Hour * 1)),
					Count:      1,
				},
				{
					ConfigID:   testConfigID.String(),
					ChangeType: "AccessKeyRotation",
					Severity:   models.SeverityLow,
					Source:     "AccessAnalyzer",
					Summary:    "Access key older than 90 days detected for service account",
					Details:    types.JSON(`{"principal":"arn:aws:iam::123456:user/ci-deploy","keyAge":"127d"}`),
					CreatedAt:  lo.ToPtr(now.Add(-time.Hour * 24)),
					Count:      1,
				},
				{
					ConfigID:   testConfigID.String(),
					ChangeType: "PublicAccess",
					Severity:   models.SeverityCritical,
					Source:     "Cloudtrail",
					Summary:    "S3 bucket policy changed to allow public access",
					Details:    types.JSON(`{"resource":"arn:aws:s3:::prod-data-bucket","action":"s3:PutBucketPolicy","effect":"Allow","principal":"*"}`),
					CreatedAt:  lo.ToPtr(now.Add(-time.Hour * 3)),
					Count:      1,
				},
			}
			err = DefaultContext.DB().Create(&changes).Error
			Expect(err).To(BeNil())
		})

		AfterAll(func() {
			// Delete in FK-safe order: artifacts → run_actions → runs → changes → config items
			DefaultContext.DB().Exec("DELETE FROM artifacts WHERE playbook_run_action_id IN (SELECT id FROM playbook_run_actions WHERE playbook_run_id IN (SELECT id FROM playbook_runs WHERE config_id = ?))", testConfigID)
			DefaultContext.DB().Exec("DELETE FROM playbook_run_actions WHERE playbook_run_id IN (SELECT id FROM playbook_runs WHERE config_id = ?)", testConfigID)
			DefaultContext.DB().Where("config_id = ?", testConfigID).Delete(&models.PlaybookRun{})
			DefaultContext.DB().Delete(&models.ConfigChange{}, "config_id = ?", testConfigID.String())
			DefaultContext.DB().Delete(&models.ConfigItem{}, "id = ?", testConfigID)
			DefaultContext.DB().Where("type = ? AND tags->>'generated-by' = ?", "SecurityAudit::Finding", "ai-audit-test").Delete(&models.ConfigItem{})
		})

		It("should run AI audit and create catalog item with valid schema", func() {
			run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "action-ai-catalog", RunParams{
				ConfigID: lo.ToPtr(testConfigID),
			}, models.PlaybookRunStatusCompleted)

			var runActions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Order("start_time").Find(&runActions).Error
			Expect(err).To(BeNil())
			Expect(runActions).To(HaveLen(2))

			// Verify AI action completed
			Expect(runActions[0].Name).To(Equal("audit"))
			Expect(runActions[0].Status).To(Equal(models.PlaybookActionStatusCompleted))
			Expect(runActions[0].Result).To(HaveKey("json"))

			// Verify the AI response matches the output schema
			aiJSON := runActions[0].Result["json"].(string)
			var findings map[string]any
			err = json.Unmarshal([]byte(aiJSON), &findings)
			Expect(err).To(BeNil(), "AI response should be valid JSON")
			Expect(findings).To(HaveKey("title"))
			Expect(findings).To(HaveKey("summary"))
			Expect(findings).To(HaveKey("findings"))
			Expect(findings).To(HaveKey("totalFindings"))

			findingsArr, ok := findings["findings"].([]any)
			Expect(ok).To(BeTrue(), "findings should be an array")
			Expect(len(findingsArr)).To(BeNumerically(">", 0), "should have at least one finding")

			// Verify each finding has required fields
			for i, f := range findingsArr {
				finding, ok := f.(map[string]any)
				Expect(ok).To(BeTrue(), "finding[%d] should be an object", i)
				Expect(finding).To(HaveKey("severity"), "finding[%d] missing severity", i)
				Expect(finding).To(HaveKey("title"), "finding[%d] missing title", i)
				Expect(finding).To(HaveKey("description"), "finding[%d] missing description", i)
				Expect(finding).To(HaveKey("resource"), "finding[%d] missing resource", i)
				Expect(finding).To(HaveKey("recommendation"), "finding[%d] missing recommendation", i)
			}

			// Verify catalog action completed and created the config item
			Expect(runActions[1].Name).To(Equal("save"))
			Expect(runActions[1].Status).To(Equal(models.PlaybookActionStatusCompleted))

			// Verify the catalog config item was created in the DB
			var catalogItem models.ConfigItem
			err = DefaultContext.DB().
				Where("type = ? AND tags->>'generated-by' = ?", "SecurityAudit::Finding", "ai-audit-test").
				First(&catalogItem).Error
			Expect(err).To(BeNil(), "catalog config item should exist")
			Expect(*catalogItem.Type).To(Equal("SecurityAudit::Finding"))
			Expect(catalogItem.ConfigClass).To(Equal("SecurityAudit"))

			// Verify the config item's config field matches the AI output schema
			Expect(catalogItem.Config).ToNot(BeNil())
			var catalogConfig map[string]any
			err = json.Unmarshal([]byte(*catalogItem.Config), &catalogConfig)
			Expect(err).To(BeNil(), "catalog config should be valid JSON")
			Expect(catalogConfig).To(HaveKey("title"))
			Expect(catalogConfig).To(HaveKey("summary"))
			Expect(catalogConfig).To(HaveKey("findings"))
			Expect(catalogConfig).To(HaveKey("totalFindings"))

			// Verify the catalog config matches the AI output (compare parsed JSON to avoid whitespace differences)
			var aiParsed, catalogParsed any
			Expect(json.Unmarshal([]byte(aiJSON), &aiParsed)).To(BeNil())
			Expect(json.Unmarshal([]byte(*catalogItem.Config), &catalogParsed)).To(BeNil())
			Expect(catalogParsed).To(Equal(aiParsed))
		})
	})

	var _ = Describe("Connection permissions", Ordered, func() {
		It("playbook must not have access to the connection even though John can read the connection", func() {
			run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "action-http-unauthorized", RunParams{
				ConfigID: lo.ToPtr(dummy.EKSCluster.ID),
			}, models.PlaybookRunStatusFailed)

			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).To(BeNil())

			Expect(actions).To(HaveLen(1))
			Expect(actions[0].Status).To(Equal(models.PlaybookActionStatusFailed))
			Expect(*actions[0].Error).To(ContainSubstring("permission required on connection://mc/httpbin"))
		})

		It("should allow access to the connection", func() {
			_ = createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "action-http-authorized", RunParams{
				ConfigID: lo.ToPtr(dummy.EKSCluster.ID),
			}, models.PlaybookRunStatusCompleted)
		})
	})

	var _ = Describe("Test Listing | Run API | Approvals", Ordered, func() {
		var (
			configPlaybook    models.Playbook
			checkPlaybook     models.Playbook
			componentPlaybook models.Playbook
			agentAllPlaybook  models.Playbook
			savedRun          models.PlaybookRun
		)

		BeforeAll(func() {
			configPlaybook, _ = createPlaybook("action-approvals")
			checkPlaybook, _ = createPlaybook("action-check")
			componentPlaybook, _ = createPlaybook("action-component")
			agentAllPlaybook, _ = createPlaybook("action-agent-all")
		})

		Context("api | list playbooks ", func() {
			It("Should fetch the suitable playbook for checks", func() {
				playbooks, err := ListPlaybooksForCheck(DefaultContext, dummy.LogisticsAPIHealthHTTPCheck.ID.String())
				ExpectPlaybook(playbooks, err, checkPlaybook)

				playbooks, err = ListPlaybooksForCheck(DefaultContext, dummy.LogisticsDBCheck.ID.String())
				ExpectPlaybook(playbooks, err)
			})

			It("Should fetch the suitable playbook for components", func() {
				playbooks, err := ListPlaybooksForComponent(DefaultContext, dummy.Logistics.ID.String())
				ExpectPlaybook(playbooks, err, componentPlaybook)

				playbooks, err = ListPlaybooksForComponent(DefaultContext, dummy.LogisticsUI.ID.String())
				ExpectPlaybook(playbooks, err)
			})

			It("Should fetch the suitable playbook for configs", func() {
				playbooks, err := ListPlaybooksForConfig(DefaultContext, dummy.EKSCluster.ID.String())
				ExpectPlaybookDesc(playbooks, "playbooks for EKS cluster config", err, configPlaybook, dummy.EchoConfig)

				playbooks, err = ListPlaybooksForConfig(DefaultContext, dummy.KubernetesCluster.ID.String())
				ExpectPlaybookDesc(playbooks, "playbooks for K8s cluster config", err, dummy.EchoConfig)
			})

			It("Should fetch playbook with agent=all for configs with any agent", func() {
				// Test with KubernetesNodeA which has an agent
				// Both agentAllPlaybook (with agent=all) and EchoConfig (with name=*) should match
				playbooks, err := ListPlaybooksForConfig(DefaultContext, dummy.KubernetesNodeA.ID.String())
				ExpectPlaybookDesc(playbooks, "playbooks for kubernetes node A", err, agentAllPlaybook, dummy.EchoConfig)

				// Test with KubernetesNodeB which also has an agent
				playbooks, err = ListPlaybooksForConfig(DefaultContext, dummy.KubernetesNodeB.ID.String())
				ExpectPlaybookDesc(playbooks, "playbooks for kubernetes node B", err, agentAllPlaybook, dummy.EchoConfig)
			})
		})

		Context("parameter validation", func() {
			testData := []struct {
				name          string
				expectedError string
				param         map[string]string
			}{
				{
					name:          "must validate required parameters",
					expectedError: "missing required parameter(s): path",
					param: map[string]string{
						"name": "test",
					},
				},
				{
					name:          "must validate unknown parameters",
					expectedError: "unknown parameter(s): icon",
					param: map[string]string{
						"path": "test",
						"name": "test",
						"icon": "flux",
					},
				},
			}

			for _, td := range testData {
				It(td.name, func() {
					response, err := client.Run(sdk.RunParams{
						ID:       configPlaybook.ID.String(),
						ConfigID: dummy.EKSCluster.ID.String(),
						Params:   td.param,
					})

					if td.expectedError != "" {
						Expect(err).ToNot(BeNil())
						Expect(err.Error()).To(ContainSubstring(td.expectedError))
					} else {
						Expect(err).To(BeNil())
						Expect(response.RunID).ToNot(BeEmpty())
					}
				})
			}
		})

		It("should render params", func() {
			requestyBody := map[string]any{
				"id":        configPlaybook.ID.String(),
				"config_id": dummy.EKSCluster.ID.String(),
			}

			response, err := http.NewClient().
				Auth(dummy.JohnDoe.Name, "admin").
				R(DefaultContext).
				Header("Content-Type", "application/json").
				Post(fmt.Sprintf("%s/playbook/%s/params", server.URL, configPlaybook.ID), requestyBody)
			Expect(err).To(BeNil())

			Expect(response.StatusCode).To(Equal(200))

			var body GetParamsResponse
			err = json.NewDecoder(response.Body).Decode(&body)
			Expect(err).To(BeNil())

			Expect(len(body.Params)).To(Equal(2))
			Expect(body.Params[0].Name).To(Equal("path"))
			Expect(body.Params[1].Name).To(Equal("name"))
			Expect(string(body.Params[1].Default)).To(Equal(*dummy.EKSCluster.Name))
		})

		It("should store playbook run via API", func() {
			run := sdk.RunParams{
				ID:       configPlaybook.ID,
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path": tempPath,
					// "footer": "" // exclude this so we use the default value
				},
			}

			response, err := client.Run(run)
			Expect(err).To(BeNil())
			Expect(response).NotTo(BeNil())
			Expect(response.RunID).NotTo(BeEmpty())

			Expect(DefaultContext.DB().Where("id = ? ", response.RunID).First(&savedRun).Error).To(BeNil())

			Expect(savedRun.PlaybookID).To(Equal(configPlaybook.ID), "run should have been created for the correct playbook")
			Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
		})

		It("should have auto approved & scheduled the playbook run", func() {
			events.ConsumeAll(DefaultContext)

			waitFor(&savedRun, models.PlaybookRunStatusCompleted)
		})

		It("should ensure that the action worked correctly", func() {

			actions, err := savedRun.GetActions(DefaultContext.DB())
			Expect(err).To(BeNil())
			Expect(actions).To(HaveLen(2))

			f, err := os.ReadFile(tempPath)
			Expect(err).To(BeNil())

			Expect(string(f)).To(Equal(fmt.Sprintf("id=%s\n%s", dummy.EKSCluster.ID, *dummy.EKSCluster.Name)))
		})
	})

	var _ = Describe("parameters", Ordered, func() {
		var (
			testPlaybook models.Playbook
			savedRun     models.PlaybookRun
			tempDir      string
			tempFile     string
		)

		BeforeAll(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "playbook-*")
			Expect(err).To(BeNil())
			tempFile = filepath.Join(tempDir, "test.txt")

			testPlaybook, _ = createPlaybook("action-params")
		})

		AfterAll(func() {
			_ = os.RemoveAll(tempDir)
		})

		It("should store playbook run via API", func() {
			run := sdk.RunParams{
				ID:       testPlaybook.ID,
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path":         tempFile,
					"my_config":    dummy.EKSCluster.ID.String(),
					"my_component": dummy.Logistics.ID.String(),
				},
			}

			response, err := client.Run(run)
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Where("id = ? ", response.RunID).First(&savedRun).Error
			Expect(err).To(BeNil())

			Expect(savedRun.PlaybookID).To(Equal(testPlaybook.ID), "run should have been created for the correct playbook")
			Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
		})

		It("should have correctly used config & component fields from parameters", func() {
			waitFor(&savedRun)

			f, err := os.ReadFile(tempFile)
			Expect(err).To(BeNil())
			Expect(string(f)).To(Equal(fmt.Sprintf("%s\n%s\n", dummy.EKSCluster.ConfigClass, dummy.Logistics.Name)))
		})
	})

	var _ = Describe("retries", Ordered, func() {
		It("should retry actions", func() {
			run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "retries", RunParams{
				ConfigID: lo.ToPtr(dummy.EKSCluster.ID),
			}, models.PlaybookRunStatusFailed)
			Expect(run.Status).To(Equal(models.PlaybookRunStatusFailed), run.String(DefaultContext.DB()))

			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Order("scheduled_time ASC").Find(&actions).Error
			Expect(err).To(BeNil())

			Expect(len(actions)).To(Equal(1 + 2)) // 1 initial + 2 retries
			for i, a := range actions {
				Expect(a.RetryCount).To(Equal(i), fmt.Sprintf("[%d] action %s should have been retried %d times", i, a.Name, i))
			}
		})
	})

	var _ = Describe("action filters", Ordered, func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun
			dataFile = "/tmp/action-filter-test.txt"
			logFile  = "/tmp/action-filter-test-log.txt"
		)

		BeforeAll(func() {
			playbook, spec = createPlaybook("action-filter")
		})

		It("should execute the playbook", func() {
			run = runPlaybook(DefaultContext.WithUser(&dummy.JohnDoe), playbook, RunParams{
				ConfigID: lo.ToPtr(dummy.EKSCluster.ID),
				Params: map[string]string{
					"path":     dataFile,
					"log_path": logFile,
				},
			})
		})

		It("should have correctly ran some and skipped some of the actions", func() {
			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).To(BeNil())

			Expect(len(actions)).To(Equal(len(spec.Actions)), "All the actions must have run event if some failed")

			for _, action := range actions {
				switch action.Name {
				case "Create the file":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Create the file: %s", models.PlaybookActionStatusCompleted))

				case "Log if the file creation failed":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusSkipped), fmt.Sprintf("Log if the file creation failed: %s", models.PlaybookActionStatusSkipped))

				case "Log if the file creation succeeded":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Log if the file creation succeeded: %s", models.PlaybookActionStatusCompleted))

				case "Run a non existing command":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusFailed), fmt.Sprintf("Run a non existing command: %s", models.PlaybookActionStatusFailed))

				case "Log if the command failed":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Log if the command failed: %s", models.PlaybookActionStatusCompleted))

				case "Skip if cluster config":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusSkipped), fmt.Sprintf("Skip if cluster config: %s", models.PlaybookActionStatusSkipped))

				case "Skip this command":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusSkipped), fmt.Sprintf("Skip this command: %s", models.PlaybookActionStatusSkipped))

				case "Log the end of the playbook":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Log the end of the playbook: %s", models.PlaybookActionStatusCompleted))
				}
			}
		})

		It("should have populated the files correctly", func() {
			data, err := os.ReadFile(dataFile)
			Expect(err).To(BeNil())
			Expect(string(data)).To(Equal(models.ConfigClassCluster))

			logs, err := os.ReadFile(logFile)
			Expect(err).To(BeNil())
			Expect(string(logs)).To(Equal("File creation succeeded\nCommand failed\n==end==\n"))
		})
	})

	var _ = Describe("function | last result", Ordered, func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun
			dataFile = "/tmp/access-previous-result.txt"
		)

		BeforeAll(func() {
			playbook, spec = createPlaybook("action-last-result")
		})

		AfterAll(func() {
			summary, _ := GetPlaybookStatus(DefaultContext, run.ID)
			DefaultContext.Infof("%s", logger.Pretty(summary))
		})

		It("should store playbook run via API", func() {
			run = runPlaybook(DefaultContext.WithUser(&dummy.JohnDoe), playbook, RunParams{
				ConfigID: lo.ToPtr(dummy.EKSCluster.ID),
				Params: map[string]string{
					"path": dataFile,
				},
			})
		})

		It("should have correctly ran some and skipped some of the actions", func() {
			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).To(BeNil())

			Expect(len(actions)).To(Equal(len(spec.Actions)), "All the actions must have run event if some failed")

			expectedStatus := map[string]models.PlaybookActionStatus{
				"make dummy API call":        models.PlaybookActionStatusCompleted,
				"Notify if the count is low": models.PlaybookActionStatusSkipped,
				"Log if count is high":       models.PlaybookActionStatusCompleted,
				"Save the count":             models.PlaybookActionStatusCompleted,
			}

			for _, action := range actions {
				expected, exists := expectedStatus[action.Name]
				if !exists {
					Fail("Unexpected action: " + action.Name)
					continue
				}

				Expect(action.Status).To(Equal(expected), action.Name)
			}
		})

		It("should have populated the files correctly", func() {
			data, err := os.ReadFile(dataFile)
			Expect(err).To(BeNil())
			Expect(string(data)).To(Equal("HIGH\n20"))
		})
	})

	var _ = Describe("runners", Ordered, Label("slow"), func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun

			awsAgentUpstreamConfig upstream.UpstreamConfig
			awsAgentName           = "aws"
			awsAgent               models.Agent
			awsAgentContext        context.Context
			awsAgentDBDrop         func()

			azureAgentName           = "azure"
			azureAgentUpstreamConfig upstream.UpstreamConfig
			azureAgent               models.Agent
			azureAgentContext        context.Context
			azureAgentDBDrop         func()
		)

		BeforeAll(func() {
			playbook, spec = createPlaybook("agent-runner")

			// Setup AWS agent
			{
				newCtx, drop, err := setup.NewDB(DefaultContext, awsAgentName)
				Expect(err).To(BeNil())
				awsAgentContext = *newCtx
				awsAgentDBDrop = drop
				awsAgentContext = awsAgentContext.WithName("aws-agent").WithDBLogger("aws-agent", "info")

				// save the agent to the db
				agentPerson := &models.Person{Name: awsAgentName}
				Expect(agentPerson.Save(DefaultContext.DB())).To(BeNil())

				awsAgent = models.Agent{Name: awsAgentName, PersonID: &agentPerson.ID}
				Expect((&awsAgent).Save(DefaultContext.DB())).To(BeNil())

				awsAgentUpstreamConfig = upstream.UpstreamConfig{
					AgentName: awsAgentName,
					Host:      server.URL,
					Username:  awsAgentName,
					Password:  "dummy",
				}

				err = rbac.AddRoleForUser(agentPerson.ID.String(), "agent")
				Expect(err).To(BeNil())

				err = rbac.ReloadPolicy()
				Expect(err).To(BeNil())
			}

			// Setup Azure agent
			{
				newCtx, drop, err := setup.NewDB(DefaultContext, azureAgentName)
				Expect(err).To(BeNil())
				azureAgentContext = *newCtx
				azureAgentDBDrop = drop
				azureAgentContext = azureAgentContext.WithName("azure-agent").WithDBLogger("azure-agent", "info")

				// save the agent to the db
				agentPerson := &models.Person{Name: azureAgentName}
				Expect(agentPerson.Save(DefaultContext.DB())).To(BeNil())

				azureAgent = models.Agent{Name: azureAgentName, PersonID: &agentPerson.ID}
				Expect((&azureAgent).Save(DefaultContext.DB())).To(BeNil())

				azureAgentUpstreamConfig = upstream.UpstreamConfig{
					AgentName: azureAgentName,
					Host:      server.URL,
					Username:  azureAgentName,
					Password:  "dummy",
				}

				err = rbac.AddRoleForUser(agentPerson.ID.String(), "agent")
				Expect(err).To(BeNil())

				err = rbac.ReloadPolicy()
				Expect(err).To(BeNil())
			}
		})

		AfterAll(func() {
			if awsAgentDBDrop != nil {
				awsAgentDBDrop()
			}

			if azureAgentDBDrop != nil {
				azureAgentDBDrop()
			}
		})

		It("should execute the playbook", func() {
			run = runPlaybook(DefaultContext.WithUser(&dummy.JohnDoe), playbook, RunParams{
				ConfigID: lo.ToPtr(dummy.KubernetesNodeA.ID),
			}, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusWaiting)

			action, err := run.GetAction(DefaultContext.DB(), spec.Actions[0].Name)
			Expect(err).To(BeNil())

			// first step schedules on local
			Expect(action.AgentID).To(BeNil())

			waitFor(run, models.PlaybookRunStatusWaiting)
			action, err = run.GetAction(DefaultContext.DB(), spec.Actions[1].Name)
			Expect(err).To(BeNil())

			// second step runs on aws agent
			Expect(*action.AgentID).To(Equal(awsAgent.ID))
			Expect(action.Status).To(Equal(models.PlaybookActionStatusWaiting))
		})

		It("azure agent should not pull action meant for aws agent", func() {
			// Try to pull actions from upstream multiple times.
			for i := 0; i < 3; i++ {
				Expect(PullPlaybookAction(job.New(azureAgentContext), azureAgentUpstreamConfig)).To(BeNil())
				_, err := run.GetAgentAction(azureAgentContext.DB(), spec.Actions[1].Name)
				Expect(err).To(Not(BeNil()))
			}
		})

		It("(aws) should pull the action from the upstream", func() {
			Expect(PullPlaybookAction(job.New(awsAgentContext), awsAgentUpstreamConfig)).To(BeNil())

			action, err := run.GetAgentAction(awsAgentContext.DB(), spec.Actions[1].Name)
			Expect(err).To(BeNil())

			Expect(action.Status).To(Equal(models.PlaybookActionStatusWaiting))
		})

		It("(aws) should run the pulled action on the agent", func() {
			err := StartPlaybookConsumers(awsAgentContext)
			Expect(err).To(BeNil())

			_, err = ActionAgentConsumer(awsAgentContext)
			Expect(err).To(BeNil())

			Eventually(func() models.PlaybookActionStatus {
				action, _ := run.GetAgentAction(awsAgentContext.DB(), spec.Actions[1].Name)
				if action != nil {
					return action.Status
				}
				return "unknown"
			}, "10s", "1s").Should(Equal(models.PlaybookActionStatusCompleted))
		})

		It("(aws) should push the action result to the upstream", func() {
			pushed, err := PushPlaybookActions(awsAgentContext, awsAgentUpstreamConfig, 10)
			Expect(err).To(BeNil())
			Expect(pushed).To(Equal(1))
		})

		It("(azure) should pull the action from the upstream", func() {
			Expect(PullPlaybookAction(job.New(azureAgentContext), azureAgentUpstreamConfig)).To(BeNil())

			action, err := run.GetAgentAction(azureAgentContext.DB(), spec.Actions[2].Name)
			Expect(err).To(BeNil())

			Expect(action.Status).To(Equal(models.PlaybookActionStatusWaiting))
		})

		It("(azure) should run the pulled action on the agent", func() {
			err := StartPlaybookConsumers(azureAgentContext)
			Expect(err).To(BeNil())

			_, err = ActionAgentConsumer(azureAgentContext)
			Expect(err).To(BeNil())

			Eventually(func() models.PlaybookActionStatus {
				action, _ := run.GetAgentAction(azureAgentContext.DB(), spec.Actions[2].Name)
				if action != nil {
					return action.Status
				}
				return "unknown"
			}, "10s", "1s").Should(Equal(models.PlaybookActionStatusCompleted))
		})

		It("(azure) should push the action result to the upstream", func() {
			pushed, err := PushPlaybookActions(azureAgentContext, awsAgentUpstreamConfig, 10)
			Expect(err).To(BeNil())
			Expect(pushed).To(Equal(1))
		})

		It("should ensure that the playbook ran to completion", func() {
			waitFor(run, models.PlaybookRunStatusCompleted)
		})

		It("should ensure that the playbook ran correctly", func() {
			actions, err := run.GetActions(DefaultContext.DB())
			Expect(err).To(BeNil())

			Expect(len(actions)).To(Equal(3))
			for i := range actions {
				Expect(actions[i].Status).To(Equal(models.PlaybookActionStatusCompleted))

				switch i {
				case 0:
					Expect(actions[i].Result["stdout"]).To(Equal("class from local agent: Node"))
				case 1:
					Expect(actions[i].Result["stdout"]).To(Equal("class from aws agent: Node"))
				case 2:
					Expect(actions[i].Result["stdout"]).To(Equal("class from azure agent: Node"))
				}
			}
		})
	})

	var _ = Describe("actions", func() {
		It("exec | powershell", func() {
			if _, err := exec.LookPath("pwsh.exe"); err != nil {
				return
			}

			run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "exec-powershell", RunParams{
				ConfigID: lo.ToPtr(dummy.KubernetesNodeA.ID),
			})
			Expect(run.Status).To(Equal(models.PlaybookRunStatusCompleted), run.String(DefaultContext.DB()))
			actions, err := run.GetActions(DefaultContext.DB())
			Expect(err).To(BeNil())
			Expect(len(actions)).To(Equal(2))
			Expect(actions[0].JSON()["item"]).To(Equal(*dummy.KubernetesNodeA.Name))
			Expect(actions[1].JSON()["item"]).To(Equal(fmt.Sprintf("name=%s", *dummy.KubernetesNodeA.Name)))
		})

		It("exec | connection | kubernetes", func() {
			run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "exec-connection-kubernetes", RunParams{
				ConfigID: lo.ToPtr(dummy.KubernetesCluster.ID),
			})

			Expect(run.Status).To(Equal(models.PlaybookRunStatusCompleted), run.String(DefaultContext.DB()))
			actions, err := run.GetActions(DefaultContext.DB())
			Expect(err).To(BeNil())
			Expect(len(actions)).To(Equal(1))
			Expect(actions[0].Result["stdout"]).To(Equal("testdata/my-kube-config.yaml")) // comes from dummy.KubeScrapeConfig
		})
	})

	var _ = Describe("ContentType", Ordered, func() {
		It("should extract contentType from action results", func() {
			run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), "action-content-type", RunParams{
				ConfigID: lo.ToPtr(dummy.KubernetesCluster.ID),
			}, models.PlaybookRunStatusCompleted)

			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Order("start_time ASC").Find(&actions).Error
			Expect(err).To(BeNil())
			Expect(actions).To(HaveLen(4))

			// Action 1: exec with JSON envelope — stdout should be unwrapped, contentType set
			Expect(actions[0].Result["stdout"]).To(Equal("# My Report"))
			Expect(actions[0].Result["contentType"]).To(Equal("text/markdown"))

			// Action 2: spec-level contentType override on plain output
			Expect(actions[1].Result["contentType"]).To(Equal("application/json"))

			// Action 3: plain stdout — no contentType key
			Expect(actions[2].Result).NotTo(HaveKey("contentType"))

			// Action 4: envelope overridden by spec contentType
			Expect(actions[3].Result["stdout"]).To(Equal("overridden"))
			Expect(actions[3].Result["contentType"]).To(Equal("text/plain"))
		})
	})

	var _ = Describe("Secret Parameters", Ordered, func() {
		XIt("should not leak secrets in action output", func() {
			run := createAndRunWithSecretParams(DefaultContext.WithUser(&dummy.JohnDoe), "action-secret-params", RunParams{
				ConfigID: lo.ToPtr(dummy.EKSCluster.ID),
			}, "super_secret_value", models.PlaybookRunStatusCompleted)

			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).To(BeNil())
			Expect(actions).To(HaveLen(1))

			// Stdout should have secret scrubbed
			stdout := actions[0].Result["stdout"].(string)
			Expect(stdout).ToNot(ContainSubstring("super_secret_value"), "stdout should not contain plaintext secret")
			Expect(stdout).To(ContainSubstring("[REDACTED]"), "stdout should contain redacted placeholder")
		})
	})

	var _ = Describe("spec runner", func() {
		type testData struct {
			name        string
			description string
			status      models.PlaybookRunStatus
			params      RunParams
			extra       func(run *models.PlaybookRun)
		}

		tests := []testData{
			{
				name:        "bad-action-spec",
				status:      models.PlaybookRunStatusFailed,
				description: "invalid action spec should fail",
				params: RunParams{
					ConfigID: &dummy.EKSCluster.ID,
				},
				extra: func(run *models.PlaybookRun) {
					var action models.PlaybookRunAction
					err := DefaultContext.DB().Where("playbook_run_id = ? ", run.ID).First(&action).Error
					Expect(err).To(BeNil())
					Expect(lo.FromPtrOr(action.Error, "")).NotTo(BeEmpty())
					Expect(action.Status).To(Equal(models.PlaybookActionStatusFailed))
				},
			},
			{
				name:   "bad-spec",
				status: models.PlaybookRunStatusFailed,
				params: RunParams{
					ConfigID: &dummy.EKSCluster.ID,
				},
				description: "invalid spec should fail",
				extra: func(run *models.PlaybookRun) {
					Expect(run.Error).ToNot(BeNil())
				},
			},
		}

		for _, test := range tests {
			It(test.description, func() {
				run := createAndRun(DefaultContext.WithUser(&dummy.JohnDoe), test.name, test.params, test.status)
				if test.extra != nil {
					test.extra(run)
				}
			})
		}
	})

	var _ = Describe("unauthorized playbooks and/or resources", func() {
		It("should deny playbooks to unauthorized users", func() {
			_, err := createAndRunNoWait(DefaultContext.WithUser(&dummy.JohnWick), "exec-powershell", RunParams{
				ConfigID: lo.ToPtr(dummy.KubernetesNodeA.ID),
			})
			Expect(err).To(Not(BeNil()))
			oe, _ := oops.AsOops(err)
			Expect(oe.Code()).To(Equal(dutyApi.EFORBIDDEN))
		})

		It("John can run any playbook but not this one specifically", func() {
			_, err := createAndRunNoWait(DefaultContext.WithUser(&dummy.JohnDoe), "echo", RunParams{
				ConfigID: lo.ToPtr(dummy.KubernetesNodeA.ID),
			})
			Expect(err).To(Not(BeNil()))
			oe, _ := oops.AsOops(err)
			Expect(oe.Code()).To(Equal(dutyApi.EFORBIDDEN))
		})

		It("John can run the playbook but not on this resource", func() {
			_, err := createAndRunNoWait(DefaultContext.WithUser(&dummy.JohnDoe), "exec-powershell", RunParams{
				ConfigID: lo.ToPtr(dummy.KubernetesNodeB.ID),
			})
			Expect(err).To(Not(BeNil()))
			oe, _ := oops.AsOops(err)
			Expect(oe.Code()).To(Equal(dutyApi.EFORBIDDEN))
		})
	})
})

func createAndRun(ctx context.Context, name string, params RunParams, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	playbook, _ := createPlaybook(name)
	Expect(testdata.LoadPermissions(ctx)).To(BeNil())
	return runPlaybook(ctx, playbook, params, statuses...)
}

func createAndRunNoWait(ctx context.Context, name string, params RunParams) (*models.PlaybookRun, error) {
	playbook, _ := createPlaybook(name)
	return Run(ctx, &playbook, params)
}

func createAndRunWithSecretParams(ctx context.Context, name string, params RunParams, secretValue string, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	playbook, _ := createPlaybook(name)
	Expect(testdata.LoadPermissions(ctx)).To(BeNil())

	// Set the password param with the secret value (will be encrypted by Run)
	if params.Params == nil {
		params.Params = make(PlaybookRuntimeParameters)
	}
	params.Params["password"] = secretValue

	return runPlaybook(ctx, playbook, params, statuses...)
}

func runPlaybook(ctx context.Context, playbook models.Playbook, params RunParams, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	run, err := Run(ctx, &playbook, params)
	Expect(err).To(BeNil())
	return waitFor(run, statuses...)
}

func waitFor(run *models.PlaybookRun, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	s := append([]models.PlaybookRunStatus{}, statuses...)
	if len(s) == 0 {
		s = append(s, models.PlaybookRunStatusFailed, models.PlaybookRunStatusCompleted)
	}

	var savedRun *models.PlaybookRun
	Eventually(func(g Gomega) models.PlaybookRunStatus {
		err := DefaultContext.DB().Where("id = ? ", run.ID).First(&savedRun).Error
		Expect(err).ToNot(HaveOccurred())

		events.ConsumeAll(DefaultContext)
		_, err = RunConsumer(DefaultContext)
		if err != nil {
			DefaultContext.Errorf("%+v", err)
		}

		if savedRun != nil {
			return savedRun.Status
		}

		return models.PlaybookRunStatus("Unknown")

	}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(BeElementOf(s))

	return savedRun
}

func createPlaybook(name string) (models.Playbook, v1.PlaybookSpec) {
	var spec v1.Playbook
	specContent, err := os.ReadFile(fmt.Sprintf("testdata/%s.yaml", name))
	Expect(err).To(BeNil())

	err = yamlutil.Unmarshal(specContent, &spec)
	Expect(err).To(BeNil())

	specJSON, err := json.Marshal(spec.Spec)
	Expect(err).To(BeNil())

	playbook := &models.Playbook{
		Namespace: lo.CoalesceOrEmpty(spec.Namespace, "default"),
		Name:      spec.Name,
		Spec:      specJSON,
		Source:    models.SourceConfigFile,
	}

	if spec.GetUID() != "" {
		playbook.ID = uuid.MustParse(string(spec.GetUID()))
	}

	Expect(playbook.Save(DefaultContext.DB())).To(BeNil())
	return *playbook, spec.Spec
}

func ExpectPlaybookDesc(list []api.PlaybookListItem, desc string, err error, playbooks ...models.Playbook) {
	Expect(err).To(BeNil())
	Expect(lo.Map(list, func(l api.PlaybookListItem, _ int) string {
		return l.ID.String() + "/" + l.Name
	})).
		To(ConsistOf(lo.Map(playbooks, func(p models.Playbook, _ int) string { return p.ID.String() + "/" + p.Name })), desc)
}

func ExpectPlaybook(list []api.PlaybookListItem, err error, playbooks ...models.Playbook) {
	Expect(err).To(BeNil())
	Expect(lo.Map(list, func(l api.PlaybookListItem, _ int) string {
		return l.ID.String() + "/" + l.Name
	})).
		To(ConsistOf(lo.Map(playbooks, func(p models.Playbook, _ int) string { return p.ID.String() + "/" + p.Name })))
}

func ExpectOKResponse(response *http.Response) {
	var runResp dutyApi.HTTPError
	if err := json.NewDecoder(response.Body).Decode(&runResp); err == nil {
		Expect(runResp.Err).To(BeEmpty())
	}
	Expect(response.IsOK()).To(BeTrue())
}

func ExpectErrorResponse(response *http.Response, err string) {
	var runResp dutyApi.HTTPError
	if err := json.NewDecoder(response.Body).Decode(&runResp); err != nil {
		Expect(runResp.Err).To(Equal(err), runResp)
	}
	Expect(response.IsOK()).To(BeFalse())
}
