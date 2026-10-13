package playbook

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = ginkgo.Describe("Work no person started", func() {
	countRuns := func(playbookID uuid.UUID) int64 {
		ginkgo.GinkgoHelper()
		var count int64
		Expect(DefaultContext.DB().Model(&models.PlaybookRun{}).Where("playbook_id = ?", playbookID).Count(&count).Error).To(Succeed())
		return count
	}

	ginkgo.Describe("a scheduled run", func() {
		scheduled := v1.PlaybookSpec{
			On: &v1.PlaybookTrigger{Schedule: []v1.PlaybookTriggerSchedule{{Schedule: "@every 1h"}}},
			Actions: []v1.PlaybookAction{
				{Name: "echo", Exec: &v1.ExecAction{Script: "echo hi"}},
			},
		}

		ginkgo.It("isn't created without playbook:run on the playbook itself, and the refusal is in its job history", func() {
			playbook := saveScheduledPlaybookForTest("scheduled-unbound", scheduled)

			triggerScheduledRun(DefaultContext, playbook.ID, 0, "@every 1h")

			Expect(countRuns(playbook.ID)).To(BeZero())
			expectScheduledRunJobHistory(playbook.ID, fmt.Sprintf("access denied to subject(%s): cannot run playbook", playbook.ID))
		})

		ginkgo.It("is created as the playbook when it's bound to playbook:run on itself", func() {
			playbook := saveScheduledPlaybookForTest("scheduled-bound", scheduled)
			ginkgo.DeferCleanup(bindPlaybook(playbook, runOnItself(playbook, nil)))

			triggerScheduledRun(DefaultContext, playbook.ID, 0, "@every 1h")

			var run models.PlaybookRun
			Expect(DefaultContext.DB().Where("playbook_id = ?", playbook.ID).First(&run).Error).To(Succeed())
			Expect(run.CreatedBy).To(BeNil(), "no person, and not the System user, started the run")
		})
	})

	ginkgo.Describe("an event-triggered run", ginkgo.Ordered, func() {
		var (
			playbook models.Playbook
			config   models.ConfigItem
		)

		handleWarning := func() {
			ginkgo.GinkgoHelper()
			scheduler := playbookScheduler{Ring: events.NewEventRing(10)}
			Expect(scheduler.Handle(DefaultContext, models.Event{Name: api.EventConfigWarning, EventID: config.ID})).To(Succeed())
		}

		ginkgo.BeforeAll(func() {
			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("caller-less-target"),
				ConfigClass: models.ConfigClassPod,
				Type:        lo.ToPtr("Kubernetes::Pod"),
			}
			Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())

			playbook = saveScheduledPlaybookForTest("event-triggered", v1.PlaybookSpec{
				On: &v1.PlaybookTrigger{PlaybookTriggerEvents: v1.PlaybookTriggerEvents{
					Config: []v1.PlaybookTriggerEvent{{Event: "warning", Filter: fmt.Sprintf("config.id == '%s'", config.ID)}},
				}},
				Actions: []v1.PlaybookAction{
					{Name: "echo", Exec: &v1.ExecAction{Script: "echo {{.config.name}}"}},
				},
			})
		})

		ginkgo.AfterAll(func() {
			Expect(DefaultContext.DB().Delete(&models.PlaybookRun{}, "playbook_id = ?", playbook.ID).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(&config).Error).To(Succeed())
		})

		ginkgo.It("isn't created on a target the playbook may not read", func() {
			ginkgo.DeferCleanup(bindPlaybook(playbook, runOnItself(playbook, &allConfigs)))

			handleWarning()

			Expect(countRuns(playbook.ID)).To(BeZero())
			expectScheduledRunJobHistory(playbook.ID, "read access to resource not allowed")
		})

		ginkgo.It("is created on a target the playbook may read", func() {
			ginkgo.DeferCleanup(bindPlaybook(playbook,
				runOnItself(playbook, &allConfigs),
				playbookGrant{Action: policy.ActionRead, Resource: allConfigs},
			))

			handleWarning()

			var run models.PlaybookRun
			Expect(DefaultContext.DB().Where("playbook_id = ?", playbook.ID).First(&run).Error).To(Succeed())
			Expect(run.ConfigID).To(Equal(&config.ID))
			Expect(run.CreatedBy).To(BeNil())
		})
	})

	ginkgo.Describe("an action filling in a connection's credentials", ginkgo.Ordered, func() {
		const password = "caller-less-password"

		var (
			playbook models.Playbook
			conn     models.Connection
			server   *httptest.Server
			ref      string
		)

		ginkgo.BeforeAll(func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, p, ok := r.BasicAuth(); !ok || p != password {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			conn = models.Connection{
				ID:        uuid.New(),
				Name:      "caller-less-http",
				Namespace: "default",
				Source:    models.SourceUI,
				Type:      models.ConnectionTypeHTTP,
				URL:       server.URL,
				Username:  "caller-less",
				Password:  password,
			}
			Expect(DefaultContext.DB().Create(&conn).Error).To(Succeed())
			ref = fmt.Sprintf("connection://%s/%s", conn.Namespace, conn.Name)

			playbook = saveScheduledPlaybookForTest("connection-user", v1.PlaybookSpec{
				Actions: []v1.PlaybookAction{{Name: "echo", Exec: &v1.ExecAction{Script: "echo hi"}}},
			})
		})

		ginkgo.AfterAll(func() {
			server.Close()
			Expect(DefaultContext.DB().Delete(&conn).Error).To(Succeed())
		})

		asPlaybook := func() context.Context { return DefaultContext.WithSubject(playbook.ID.String()) }

		ginkgo.It("fails without connection:use, even with read", func() {
			ginkgo.DeferCleanup(bindPlaybook(playbook, playbookGrant{
				Action:   policy.ActionRead,
				Resource: v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}},
			}))

			_, err := (&actions.HTTP{}).Run(asPlaybook(), v1.HTTPAction{HTTPConnection: connection.HTTPConnection{ConnectionName: ref}})
			Expect(err).To(MatchError(ContainSubstring("connection:use required on connection default/caller-less-http")))

			_, err = (&actions.ExecAction{}).Run(asPlaybook(), v1.ExecAction{
				Script:      "echo hi",
				Connections: connection.ExecConnections{AWS: &connection.AWSConnection{ConnectionName: ref}},
			})
			Expect(err).To(MatchError(ContainSubstring("connection:use required on connection default/caller-less-http")))
		})

		ginkgo.It("is refused a connection that doesn't exist, whatever duty has cached for it", func() {
			err := rbac.AuthorizeConnectionRefUse(asPlaybook(), "connection://default/caller-less-missing")
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})

		ginkgo.It("needs connection:use on the connections a connection's properties name too", func() {
			aws := models.Connection{
				ID: uuid.New(), Name: "caller-less-aws", Namespace: "default", Source: models.SourceUI,
				Type: models.ConnectionTypeAWS, Username: "access-key", Password: "secret-key",
			}
			signed := models.Connection{
				ID: uuid.New(), Name: "caller-less-signed", Namespace: "default", Source: models.SourceUI,
				Type: models.ConnectionTypeHTTP, URL: server.URL,
				Properties: types.JSONStringMap{"awsSigV4": `{"connection":"connection://default/caller-less-aws","service":"es"}`},
			}
			Expect(DefaultContext.DB().Create(&aws).Error).To(Succeed())
			Expect(DefaultContext.DB().Create(&signed).Error).To(Succeed())
			ginkgo.DeferCleanup(func() {
				Expect(DefaultContext.DB().Delete(&signed).Error).To(Succeed())
				Expect(DefaultContext.DB().Delete(&aws).Error).To(Succeed())
			})

			ginkgo.DeferCleanup(bindPlaybook(playbook, playbookGrant{
				Action:   adapter.ActionConnectionUse,
				Resource: v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: signed.Name, Namespace: signed.Namespace}}},
			}))

			_, err := (&actions.HTTP{}).Run(asPlaybook(), v1.HTTPAction{HTTPConnection: connection.HTTPConnection{ConnectionName: "connection://default/caller-less-signed"}})
			Expect(err).To(MatchError(ContainSubstring("connection:use required on connection default/caller-less-aws")))
		})

		ginkgo.It("succeeds with connection:use on the connection", func() {
			ginkgo.DeferCleanup(bindPlaybook(playbook, playbookGrant{
				Action:   adapter.ActionConnectionUse,
				Resource: v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: conn.Name, Namespace: conn.Namespace}}},
			}))

			result, err := (&actions.HTTP{}).Run(asPlaybook(), v1.HTTPAction{HTTPConnection: connection.HTTPConnection{ConnectionName: ref}})
			Expect(err).To(Succeed())
			Expect(result.StatusCode).To(Equal(http.StatusOK), "the connection's credentials were filled in")
		})
	})
})
