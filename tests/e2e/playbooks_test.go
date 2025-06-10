package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/logs"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/sdk"
	"github.com/flanksource/incident-commander/playbook/testdata"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var (
	lokiEndpoint       = lo.CoalesceOrEmpty(os.Getenv("LOKI_ENDPOINT"), "http://localhost:3100")
	openSearchEndpoint = lo.CoalesceOrEmpty(os.Getenv("OPENSEARCH_ENDPOINT"), "http://localhost:9200")
)

func waitFor(ctx context.Context, run *models.PlaybookRun, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	s := slices.Clone(statuses)
	if len(s) == 0 {
		s = append(s, models.PlaybookRunStatusFailed, models.PlaybookRunStatusCompleted)
	}

	var savedRun *models.PlaybookRun
	Eventually(func(g Gomega) models.PlaybookRunStatus {
		err := ctx.DB().Where("id = ? ", run.ID).First(&savedRun).Error
		Expect(err).ToNot(HaveOccurred())

		events.ConsumeAll(ctx)
		_, err = playbook.RunConsumer(ctx)
		if err != nil {
			ctx.Errorf("%+v", err)
		}

		if savedRun != nil {
			return savedRun.Status
		}

		return models.PlaybookRunStatus("Unknown")
	}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(BeElementOf(s))

	return savedRun
}

func waitForLokiLogs() {
	client := http.NewClient()

	Eventually(func(g Gomega) {
		endpoint, err := url.JoinPath(lokiEndpoint, "loki/api/v1/query_range")
		g.Expect(err).To(BeNil())

		// Query parameters
		lokiQuery := `{environment="production"}`

		params := url.Values{}
		params.Set("query", lokiQuery)
		params.Set("start", time.Now().Add(time.Hour*-24).Format(time.RFC3339)) // Before our test timestamps
		params.Set("end", time.Now().Format(time.RFC3339))                      // After our test timestamps
		params.Set("limit", "100")

		queryURL := endpoint + "?" + params.Encode()
		resp, err := client.R(DefaultContext).Get(queryURL)
		g.Expect(err).To(BeNil())
		g.Expect(resp.IsOK()).To(BeTrue())

		var result map[string]any
		err = resp.Into(&result)
		g.Expect(err).To(BeNil())

		// Check that we got results
		data, exists := result["data"].(map[string]any)
		g.Expect(exists).To(BeTrue(), "Expected 'data' field in Loki response")

		results, exists := data["result"].([]any)
		g.Expect(exists).To(BeTrue(), "Expected 'result' field in Loki data")
		g.Expect(len(results)).To(BeNumerically(">", 0), "Expected at least one log entry for query: %s", lokiQuery)
	}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
}

var _ = ginkgo.Describe("Playbooks", ginkgo.Ordered, func() {
	var _ = ginkgo.Context("logs", func() {
		ginkgo.BeforeAll(func() {
			// Loki seeding
			{
				lokiContent, err := os.ReadFile("setup/seed-loki.json")
				Expect(err).To(BeNil())

				baseTimeLoki := time.Now().Add(-5 * time.Minute)
				timestamp1Loki := fmt.Sprintf("%d", baseTimeLoki.UnixNano())
				timestamp2Loki := fmt.Sprintf("%d", baseTimeLoki.Add(1*time.Second).UnixNano())
				timestamp3Loki := fmt.Sprintf("%d", baseTimeLoki.Add(2*time.Second).UnixNano())

				updatedLokiContent := string(lokiContent)
				updatedLokiContent = strings.ReplaceAll(updatedLokiContent, "{{TIMESTAMP_1}}", timestamp1Loki)
				updatedLokiContent = strings.ReplaceAll(updatedLokiContent, "{{TIMESTAMP_2}}", timestamp2Loki)
				updatedLokiContent = strings.ReplaceAll(updatedLokiContent, "{{TIMESTAMP_3}}", timestamp3Loki)

				lokiPushEndpoint, err := url.JoinPath(lokiEndpoint, "loki/api/v1/push")
				Expect(err).To(BeNil())

				responseLoki, err := http.NewClient().R(DefaultContext).Header("Content-Type", "application/json").Post(lokiPushEndpoint, updatedLokiContent)
				Expect(err).To(BeNil())
				Expect(responseLoki.IsOK()).To(BeTrue())

				waitForLokiLogs()
			}

			// OpenSearch seeding
			{
				opensearchContent, err := os.ReadFile("setup/seed-opensearch.json")
				Expect(err).To(BeNil())

				opensearchBulkEndpoint, err := url.JoinPath(openSearchEndpoint, "_bulk")
				Expect(err).To(BeNil())

				responseOpenSearch, err := http.NewClient().R(DefaultContext).Header("Content-Type", "application/json").Post(opensearchBulkEndpoint, opensearchContent)
				Expect(err).To(BeNil())

				opensearchBodyBytes, err := io.ReadAll(responseOpenSearch.Body)
				Expect(err).To(BeNil())
				responseOpenSearch.Body.Close()

				Expect(responseOpenSearch.IsOK()).To(BeTrue(), "OpenSearch bulk insert failed with status: %s and body: %s", responseOpenSearch.Response.Status, string(opensearchBodyBytes))

				// Check for errors in the bulk response
				var bulkResponse map[string]any
				err = json.Unmarshal(opensearchBodyBytes, &bulkResponse)
				Expect(err).To(BeNil())
				Expect(bulkResponse["errors"]).To(Equal(false), "OpenSearch bulk insert had errors: %+v", bulkResponse)
			}
		})

		base := "../../playbook/testdata/e2e/"

		entries, err := os.ReadDir(base)
		Expect(err).To(BeNil())

		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), "yaml") {
				continue
			}

			ginkgo.It(fmt.Sprintf("should save & schedule a run for the fixture: %s", entry.Name()), func() {
				fullpath := filepath.Join(base, entry.Name())
				content, err := os.ReadFile(fullpath)
				Expect(err).To(BeNil())

				var customResource v1.Playbook
				err = yaml.Unmarshal(content, &customResource)
				Expect(err).To(BeNil())

				if customResource.UID == "" {
					customResource.UID = types.UID(uuid.New().String())
				}

				err = db.PersistPlaybookFromCRD(DefaultContext, &customResource)
				Expect(err).To(BeNil())

				Expect(testdata.LoadPermissions(DefaultContext)).To(BeNil())

				runParam := sdk.RunParams{
					ID:       customResource.UID,
					ConfigID: dummy.EKSCluster.ID,
					Params:   map[string]string{},
				}
				response, err := client.Run(runParam)
				Expect(err).To(BeNil())

				var run models.PlaybookRun
				err = DefaultContext.DB().Where("id = ?", response.RunID).Find(&run).Error
				Expect(err).To(BeNil())

				completedRun := waitFor(DefaultContext, &run, models.PlaybookRunStatusCompleted, models.PlaybookRunStatusFailed)
				Expect(completedRun.Status).To(Equal(models.PlaybookRunStatusCompleted))

				var actions []models.PlaybookRunAction
				err = DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
				Expect(err).To(BeNil())

				Expect(actions).To(HaveLen(len(customResource.Spec.Actions)))

				actionIDs := lo.Map(actions, func(item models.PlaybookRunAction, _ int) string {
					return item.ID.String()
				})
				allArtifacts, err := artifacts.GetArtifactContents(DefaultContext, actionIDs...)
				Expect(err).To(BeNil())

				for _, artif := range allArtifacts {
					var output strings.Builder
					var lines []logs.LogLine
					err := json.Unmarshal(artif.Content, &lines)
					Expect(err).To(BeNil())
					for _, line := range lines {
						output.WriteString(line.Message)
						output.WriteString("\n")
					}

					actionDetails, found := lo.Find(actions, func(a models.PlaybookRunAction) bool {
						return a.ID.String() == artif.ActionID
					})
					Expect(found).To(BeTrue())
					expected := customResource.Annotations[fmt.Sprintf("expected-%s", actionDetails.Name)]
					Expect(output.String()).To(Equal(expected), fmt.Sprintf("action result: %s", actionDetails.Name))
				}
			})
		}
	})
})
