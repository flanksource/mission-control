package e2e

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/robfig/cron/v3"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/sdk"
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
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeElementOf(s))

	return savedRun
}

func waitForLokiLogs() {
	client := http.NewClient()

	Eventually(func(g Gomega) {
		endpoint, err := url.JoinPath(lokiEndpoint, "loki/api/v1/query_range")
		g.Expect(err).To(BeNil())

		params := url.Values{}
		params.Set("query", `{environment="production"}`)
		params.Set("start", time.Now().Add(time.Hour*-24).Format(time.RFC3339))
		params.Set("end", time.Now().Format(time.RFC3339))
		params.Set("limit", "100")

		resp, err := client.R(DefaultContext).Get(endpoint + "?" + params.Encode())
		g.Expect(err).To(BeNil())
		g.Expect(resp.IsOK()).To(BeTrue())

		var result map[string]any
		err = resp.Into(&result)
		g.Expect(err).To(BeNil())

		data, exists := result["data"].(map[string]any)
		g.Expect(exists).To(BeTrue(), "Expected 'data' field in Loki response")

		results, exists := data["result"].([]any)
		g.Expect(exists).To(BeTrue(), "Expected 'result' field in Loki data")
		g.Expect(len(results)).To(BeNumerically(">", 0))
	}).WithTimeout(60 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
}

var _ = ginkgo.Describe("Playbooks", ginkgo.Ordered, func() {
	var viewRef string

	ginkgo.BeforeAll(func() {
		viewRef = fmt.Sprintf("%s/%s", dummy.PodView.Namespace, dummy.PodView.Name)
	})

	ginkgo.AfterAll(func() {
		for _, h := range allSetupHandlers {
			h.Cleanup()
		}
	})

	fixtures, err := filepath.Glob("testdata/playbooks/*.yaml")
	if err != nil {
		panic(fmt.Sprintf("failed to glob playbook fixtures: %v", err))
	}

	// FIXME: Disable email playbooks for now
	// Fails most of the time in CI
	skipped := []string{"email-report"}
	fixtures = lo.Filter(fixtures, func(f string, _ int) bool {
		return !lo.Contains(skipped, f)
	})

	for _, fixturePath := range fixtures {
		setup := peekFixtureSetup(fixturePath)
		name := strings.TrimSuffix(filepath.Base(fixturePath), ".yaml")

		var decorators []any
		for _, h := range allSetupHandlers {
			for _, l := range h.Labels(setup) {
				decorators = append(decorators, ginkgo.Label(l))
			}
		}

		decorators = append(decorators, func() {
			f := loadPlaybookFixture(fixturePath)

			pb := fixtureToPlaybook(f)
			Expect(db.PersistPlaybookFromCRD(DefaultContext, &pb)).To(Succeed())

			fctx := &fixtureContext{
				Fixture:  &f,
				Path:     fixturePath,
				Vars:     map[string]string{"viewRef": viewRef},
				CelEnv:   map[string]any{},
				Playbook: &pb,
			}

			for _, h := range allSetupHandlers {
				h.Handle(fctx)
			}

			params := resolveParams(f.Params, fctx.Vars)
			run, err := client.Run(sdk.RunParams{
				ID:       pb.UID,
				ConfigID: resolveConfigID(f.Config),
				Params:   params,
			})
			Expect(err).ToNot(HaveOccurred())

			var pbRun models.PlaybookRun
			Expect(DefaultContext.DB().Where("id = ?", run.RunID).First(&pbRun).Error).To(Succeed())

			completedRun := waitFor(DefaultContext, &pbRun)

			var actions []models.PlaybookRunAction
			Expect(DefaultContext.DB().Where("playbook_run_id = ?", pbRun.ID).Order("start_time").Find(&actions).Error).To(Succeed())

			compareOutput(f.Output, completedRun, actions)

			if collect, ok := fctx.CelEnv["_smtpCollect"].(func()); ok {
				collect()
			}
			evalAssertions(f.Assertions, fctx.CelEnv)
		})

		ginkgo.It(name, decorators...)
	}

	ginkgo.It("runs a scheduled playbook", ginkgo.Label("slow"), func() {
		content, err := os.ReadFile("testdata/scheduled-report-playbook.yaml")
		Expect(err).ToNot(HaveOccurred())

		var pb v1.Playbook
		Expect(yaml.Unmarshal(content, &pb)).To(Succeed())
		if pb.UID == "" {
			pb.UID = types.UID(uuid.NewString())
		}

		Expect(db.PersistPlaybookFromCRD(DefaultContext, &pb)).To(Succeed())

		// Grant artifact access for the scheduled playbook
		playbookRef := fmt.Sprintf("%s/%s", pb.Namespace, pb.Name)
		perm := &v1.Permission{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("allow-%s-artifacts", pb.Name),
				Namespace: pb.Namespace,
				UID:       types.UID(uuid.NewString()),
			},
			Spec: v1.PermissionSpec{
				Description: fmt.Sprintf("allow %s to read artifacts connection", playbookRef),
				Subject:     v1.PermissionSubject{Playbook: playbookRef},
				Actions:     []string{"read"},
				Object: v1.PermissionObject{
					Selectors: dutyRBAC.Selectors{
						Connections: []dutyTypes.ResourceSelector{{Name: "artifacts", Namespace: "default"}},
					},
				},
			},
		}
		Expect(db.PersistPermissionFromCRD(DefaultContext, perm)).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())

		playbookModel, err := pb.ToModel()
		Expect(err).ToNot(HaveOccurred())

		viewRef := fmt.Sprintf("%s/%s", dummy.PodView.Namespace, dummy.PodView.Name)
		Expect(DefaultContext.DB().Exec(
			`UPDATE playbooks SET spec = jsonb_set(spec, '{on,schedule,0,parameters,view}', to_jsonb(?::text)) WHERE id = ?`,
			viewRef, playbookModel.ID,
		).Error).To(Succeed())

		testScheduler := cron.New()
		testScheduler.Start()
		defer testScheduler.Stop()

		Expect(playbook.SyncPlaybookSchedulesForTest(DefaultContext, testScheduler)).To(Succeed())
		Expect(testScheduler.Entries()).ToNot(BeEmpty(), "expected cron entries to be registered")

		Eventually(func() int64 {
			var count int64
			DefaultContext.DB().Model(&models.PlaybookRun{}).Where("playbook_id = ?", playbookModel.ID).Count(&count)
			return count
		}, 30*time.Second, time.Second).Should(BeNumerically(">=", 1))

		_ = DefaultContext.DB().Model(&models.Playbook{}).
			Where("id = ?", playbookModel.ID).
			Update("deleted_at", duty.Now()).Error
		_ = playbook.SyncPlaybookSchedulesForTest(DefaultContext, testScheduler)
	})
})
