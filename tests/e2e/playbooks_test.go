package e2e

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/sdk"
	"github.com/flanksource/incident-commander/playbook/testdata"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/types"
)

var (
	lokiEndpoint = lo.CoalesceOrEmpty(os.Getenv("LOKI_ENDPOINT"), "http://localhost:3100")
)

func waitFor(ctx context.Context, run *models.PlaybookRun, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	s := append([]models.PlaybookRunStatus{}, statuses...)
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

var _ = ginkgo.Describe("Playbooks", ginkgo.Ordered, func() {
	var _ = ginkgo.Context("logs", func() {
		ginkgo.BeforeAll(func() {
			content, err := os.ReadFile("setup/seed-loki.json")
			Expect(err).To(BeNil())

			endpoint, err := url.JoinPath(lokiEndpoint, "loki/api/v1/push")
			Expect(err).To(BeNil())

			response, err := http.NewClient().R(DefaultContext).Header("Content-Type", "application/json").
				Post(endpoint, string(content))
			Expect(err).To(BeNil())
			Expect(response.IsOK()).To(BeTrue())
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

				Expect(actions).To(HaveLen(2))

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
