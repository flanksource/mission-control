// Tests for the action consumer's claim-then-execute split (streaming), the failing-action
// regression, and the orphaned-action reaper for both local and agent actions.
package playbook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	icapi "github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/flanksource/incident-commander/playbook/runner"
)

// ancientTime is well before any action created during the suite, so an action stamped with
// it sorts first in getNextAction/getNextActionForAgent regardless of other rows in the
// shared test database, making claims deterministic.
var ancientTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// scheduleLocalAction persists a playbook, run and a single scheduled action whose spec runs
// the given action, ready for the local ActionConsumer to claim. It bypasses Run()'s RBAC by
// writing the rows directly. The action's scheduled_time is backdated so claimNextAction picks
// it deterministically.
func scheduleLocalAction(ctx context.Context, name string, action v1.PlaybookAction) (models.PlaybookRun, models.PlaybookRunAction) {
	spec, err := json.Marshal(v1.PlaybookSpec{Actions: []v1.PlaybookAction{action}})
	Expect(err).To(BeNil())

	playbook := models.Playbook{
		Namespace: "default",
		Name:      name,
		Spec:      spec,
		Source:    models.SourceConfigFile,
	}
	Expect(playbook.Save(ctx.DB())).To(BeNil())

	run := models.PlaybookRun{
		PlaybookID:    playbook.ID,
		Spec:          spec,
		Status:        models.PlaybookRunStatusRunning,
		ScheduledTime: time.Now(),
		Timeout:       30 * time.Minute,
	}
	Expect(ctx.DB().Create(&run).Error).To(BeNil())

	runAction := models.PlaybookRunAction{
		PlaybookRunID: run.ID,
		Name:          action.Name,
		Status:        models.PlaybookActionStatusScheduled,
		ScheduledTime: ancientTime,
	}
	Expect(ctx.DB().Create(&runAction).Error).To(BeNil())

	return run, runAction
}

func actionStatus(ctx context.Context, id uuid.UUID) models.PlaybookActionStatus {
	var got models.PlaybookRunAction
	Expect(ctx.DB().Where("id = ?", id).First(&got).Error).To(BeNil())
	return got.Status
}

var _ = ginkgo.Describe("ActionConsumer claim-then-execute", ginkgo.Ordered, func() {
	// Disable the background consumers so the specs drive claim + execution manually and can
	// observe intermediate states without races. The scheduler stays enabled but is unused here.
	ginkgo.BeforeAll(func() {
		Expect(context.UpdateProperty(DefaultContext, "playbook.runner.disabled", "true")).To(BeNil())
	})

	ginkgo.AfterAll(func() {
		Expect(context.UpdateProperty(DefaultContext, "playbook.runner.disabled", "false")).To(BeNil())
	})

	ginkgo.It("streams report logs while execution is still running", func() {
		facetRequest := make(chan struct{}, 1)
		releaseFacet := make(chan struct{}, 1)
		facetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			facetRequest <- struct{}{}
			<-releaseFacet
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>rendered</html>"))
		}))
		defer facetServer.Close()
		defer func() {
			select {
			case releaseFacet <- struct{}{}:
			default:
			}
		}()

		_, scheduled := scheduleLocalAction(DefaultContext, "stream-report", v1.PlaybookAction{
			Name: "report",
			Report: &v1.ReportAction{
				Configs:  &types.ResourceSelector{ID: dummy.KubernetesNodeA.ID.String(), Limit: 1},
				Format:   "facet-html",
				Facet:    &v1.FacetOptions{URL: facetServer.URL},
				Sections: &icapi.CatalogReportSections{},
			},
		})

		action, run, err := claimNextAction(DefaultContext)
		Expect(err).To(BeNil())
		Expect(action).ToNot(BeNil())
		Expect(action.ID).To(Equal(scheduled.ID))
		Expect(actionStatus(DefaultContext, scheduled.ID)).To(Equal(models.PlaybookActionStatusRunning))

		done := make(chan error, 1)
		go func() {
			done <- runner.RunAction(DefaultContext, run, action)
		}()
		select {
		case <-facetRequest:
		case err := <-done:
			if err == nil {
				ginkgo.Fail("report action completed before calling the facet server")
			}
			Expect(err).ToNot(HaveOccurred())
		case <-time.After(30 * time.Second):
			ginkgo.Fail("timed out waiting for the report action to call the facet server")
		}

		var streaming models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", scheduled.ID).First(&streaming).Error).To(BeNil())
		Expect(streaming.Status).To(Equal(models.PlaybookActionStatusRunning))
		Expect(streaming.Result["logs"]).To(ContainSubstring("rendering html via facet service"))

		releaseFacet <- struct{}{}
		Eventually(done, 30*time.Second).Should(Receive(BeNil()))
		Expect(actionStatus(DefaultContext, scheduled.ID)).To(Equal(models.PlaybookActionStatusCompleted))
	})

	ginkgo.It("marks a failing SQL query failed without wedging the consumer", func() {
		_, scheduled := scheduleLocalAction(DefaultContext, "stream-sql-fail",
			v1.PlaybookAction{Name: "bad-sql", SQL: &v1.SQLAction{
				Driver: "postgres",
				URL:    os.Getenv("DB_URL"),
				Query:  "SELEC 1",
			}})

		action, run, err := claimNextAction(DefaultContext)
		Expect(err).To(BeNil())
		Expect(action.ID).To(Equal(scheduled.ID))

		// Execution runs outside a transaction, so a failing action is a plain error that
		// ExecuteAndSaveAction records via action.Fail; RunAction itself returns no error.
		Expect(runner.RunAction(DefaultContext, run, action)).To(BeNil())
		Expect(actionStatus(DefaultContext, scheduled.ID)).To(Equal(models.PlaybookActionStatusFailed))

		// The failure must not poison the connection: a subsequent action runs to completion.
		_, next := scheduleLocalAction(DefaultContext, "stream-after-fail",
			v1.PlaybookAction{Name: "echo", Exec: &v1.ExecAction{Script: "echo still working"}})
		nextAction, nextRun, err := claimNextAction(DefaultContext)
		Expect(err).To(BeNil())
		Expect(nextAction.ID).To(Equal(next.ID))
		Expect(runner.RunAction(DefaultContext, nextRun, nextAction)).To(BeNil())
		Expect(actionStatus(DefaultContext, next.ID)).To(Equal(models.PlaybookActionStatusCompleted))
	})

	ginkgo.It("marks a missing playbook failed after the action is claimed", func() {
		_, scheduled := scheduleLocalAction(DefaultContext, "stream-missing-playbook",
			v1.PlaybookAction{Name: "echo", Exec: &v1.ExecAction{Script: "echo unreachable"}})

		action, run, err := claimNextAction(DefaultContext)
		Expect(err).To(BeNil())
		Expect(action.ID).To(Equal(scheduled.ID))

		action.PlaybookRunID = uuid.New()
		Expect(runner.RunAction(DefaultContext, run, action)).ToNot(BeNil())
		Expect(actionStatus(DefaultContext, scheduled.ID)).To(Equal(models.PlaybookActionStatusFailed))
	})

	ginkgo.It("recovers an action orphaned mid-execution and retries it to completion", func() {
		_, scheduled := scheduleLocalAction(DefaultContext, "stream-orphan",
			v1.PlaybookAction{Name: "echo", Exec: &v1.ExecAction{Script: "echo recovered"}})

		// Claim the action but never execute it, simulating a crash after the claim commits.
		action, _, err := claimNextAction(DefaultContext)
		Expect(err).To(BeNil())
		Expect(action.ID).To(Equal(scheduled.ID))
		Expect(actionStatus(DefaultContext, scheduled.ID)).To(Equal(models.PlaybookActionStatusRunning))

		// Backdate the claim past the orphan timeout and reap it.
		Expect(DefaultContext.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", scheduled.ID).
			Update("start_time", ancientTime).Error).To(BeNil())
		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())
		Expect(actionStatus(DefaultContext, scheduled.ID)).To(Equal(models.PlaybookActionStatusScheduled))

		// The reset action is claimable again and now runs to completion.
		retried, retriedRun, err := claimNextAction(DefaultContext)
		Expect(err).To(BeNil())
		Expect(retried.ID).To(Equal(scheduled.ID))
		Expect(runner.RunAction(DefaultContext, retriedRun, retried)).To(BeNil())
		Expect(actionStatus(DefaultContext, scheduled.ID)).To(Equal(models.PlaybookActionStatusCompleted))
	})
})

var _ = ginkgo.Describe("ActionAgentConsumer claim", ginkgo.Ordered, func() {
	ginkgo.BeforeAll(func() {
		Expect(context.UpdateProperty(DefaultContext, "playbook.runner.disabled", "true")).To(BeNil())
	})

	ginkgo.AfterAll(func() {
		Expect(context.UpdateProperty(DefaultContext, "playbook.runner.disabled", "false")).To(BeNil())
	})

	ginkgo.It("commits the running claim before execution for agent actions", func() {
		// Agent-pulled actions carry their own spec/env and a null playbook_run_id.
		runAction := models.PlaybookRunAction{
			Name:          "agent-step",
			Status:        models.PlaybookActionStatusWaiting,
			ScheduledTime: ancientTime,
		}
		// PullPlaybookAction omits both fields. playbook_run_id remains NULL while agent_id
		// receives the database's nil-UUID default.
		Expect(DefaultContext.DB().Omit("playbook_run_id", "agent_id").Create(&runAction).Error).To(BeNil())
		var stored models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", runAction.ID).First(&stored).Error).To(BeNil())
		Expect(stored.PlaybookRunID).To(Equal(uuid.Nil))
		Expect(stored.AgentID).ToNot(BeNil())
		Expect(*stored.AgentID).To(Equal(uuid.Nil))

		spec, err := json.Marshal(v1.PlaybookAction{Name: "agent-step", Exec: &v1.ExecAction{Script: "echo hi"}})
		Expect(err).To(BeNil())
		env, err := json.Marshal(actions.TemplateEnv{})
		Expect(err).To(BeNil())

		agentData := models.PlaybookActionAgentData{
			ActionID:   runAction.ID,
			RunID:      uuid.New(),
			PlaybookID: uuid.New(),
			Spec:       spec,
			Env:        env,
		}
		Expect(DefaultContext.DB().Create(&agentData).Error).To(BeNil())

		claimed, err := claimNextAgentAction(DefaultContext)
		Expect(err).To(BeNil())
		Expect(claimed).ToNot(BeNil())
		Expect(claimed.runAction.ID).To(Equal(runAction.ID))
		Expect(claimed.spec.Name).To(Equal("agent-step"))

		// The claim committed independently, so the running status is visible before execution.
		Expect(actionStatus(DefaultContext, runAction.ID)).To(Equal(models.PlaybookActionStatusRunning))
	})
})

var _ = ginkgo.Describe("Orphaned action reaper", func() {
	var run models.PlaybookRun

	newAction := func(status models.PlaybookActionStatus, startTime time.Time) models.PlaybookRunAction {
		action := models.PlaybookRunAction{
			PlaybookRunID: run.ID,
			Name:          "step",
			Status:        status,
			StartTime:     startTime,
			// scheduled in the future so the background consumer never picks it up,
			// keeping assertions free of races.
			ScheduledTime: time.Now().Add(time.Hour),
		}
		Expect(DefaultContext.DB().Create(&action).Error).To(BeNil())
		return action
	}

	ginkgo.BeforeEach(func() {
		playbook := models.Playbook{
			Namespace: "default",
			Name:      "reaper-test",
			Spec:      []byte(`{"actions":[]}`),
			Source:    models.SourceConfigFile,
		}
		Expect(playbook.Save(DefaultContext.DB())).To(BeNil())

		run = models.PlaybookRun{
			PlaybookID:    playbook.ID,
			Spec:          playbook.Spec,
			Status:        models.PlaybookRunStatusRunning,
			ScheduledTime: time.Now(),
			Timeout:       30 * time.Minute,
		}
		Expect(DefaultContext.DB().Create(&run).Error).To(BeNil())
	})

	ginkgo.It("resets a running action whose start_time is past the orphan timeout", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now().Add(-2*time.Hour))

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusScheduled))
		Expect(got.StartTime.IsZero()).To(BeTrue())
	})

	ginkgo.It("resets an orphaned agent-pulled action to waiting", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now().Add(-2*time.Hour))
		Expect(DefaultContext.DB().Exec(
			"UPDATE playbook_run_actions SET playbook_run_id = NULL, agent_id = ? WHERE id = ?",
			uuid.Nil, action.ID).Error).To(BeNil())

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusWaiting))
		Expect(got.StartTime.IsZero()).To(BeTrue())
	})

	ginkgo.It("leaves a recently started running action untouched", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now())

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusRunning))
	})

	ginkgo.It("does not touch actions in a terminal state", func() {
		action := newAction(models.PlaybookActionStatusCompleted, time.Now().Add(-2*time.Hour))

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusCompleted))
	})

	ginkgo.It("does not reschedule an action whose parent run is terminal", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now().Add(-2*time.Hour))
		Expect(DefaultContext.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).
			Update("status", models.PlaybookRunStatusTimedOut).Error).To(BeNil())

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())
		Expect(actionStatus(DefaultContext, action.ID)).To(Equal(models.PlaybookActionStatusRunning))
	})

	ginkgo.It("does not reschedule an action after its parent run timeout", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now().Add(-2*time.Hour))
		Expect(DefaultContext.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).
			Updates(map[string]any{"scheduled_time": ancientTime, "timeout": time.Minute}).Error).To(BeNil())

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())
		Expect(actionStatus(DefaultContext, action.ID)).To(Equal(models.PlaybookActionStatusRunning))
	})
})
