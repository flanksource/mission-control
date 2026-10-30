package playbook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/rbactest"
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

	ginkgo.Describe("a run started from the command line", func() {
		spec := v1.PlaybookSpec{
			Actions: []v1.PlaybookAction{{Name: "echo", Exec: &v1.ExecAction{Script: "echo hi"}}},
		}

		ginkgo.It("is checked as the playbook, never as the System person", func() {
			playbook := saveScheduledPlaybookForTest("cli-bound", spec)
			ginkgo.DeferCleanup(bindPlaybook(playbook, runOnItself(playbook, nil)))

			var system models.Person
			Expect(DefaultContext.DB().Where("name = 'System'").First(&system).Error).To(Succeed())

			recorder, stop := rbactest.RecordSubjects()
			ginkgo.DeferCleanup(stop)

			run, err := Run(RunAsPlaybook(DefaultContext, &playbook), &playbook, RunParams{})
			Expect(err).To(Succeed())
			Expect(run.CreatedBy).To(BeNil())

			Expect(recorder.Subjects()).ToNot(BeEmpty())
			Expect(recorder.Subjects()).ToNot(ContainElement(system.ID.String()))
			Expect(recorder.Subjects()).To(HaveEach(playbook.ID.String()))
		})

		ginkgo.It("isn't created without the playbook's own binding", func() {
			playbook := saveScheduledPlaybookForTest("cli-unbound", spec)

			_, err := Run(RunAsPlaybook(DefaultContext, &playbook), &playbook, RunParams{})
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("access denied to subject(%s): cannot run playbook", playbook.ID))))
			Expect(countRuns(playbook.ID)).To(BeZero())
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
			Expect(scheduler.Handle(DefaultContext, models.Event{ID: uuid.New(), Name: api.EventConfigWarning, EventID: config.ID})).To(Succeed())

			Eventually(func() int64 {
				var pending int64
				Expect(DefaultContext.DB().Model(&models.Event{}).
					Where("name = ? AND properties->>'playbook_id' = ?", api.EventPlaybookTrigger, playbook.ID.String()).
					Count(&pending).Error).To(Succeed())
				return pending
			}, "10s", "200ms").Should(BeZero())
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

			Eventually(func() models.PlaybookRunStatus {
				Expect(DefaultContext.DB().Where("id = ?", run.ID).First(&run).Error).To(Succeed())
				return run.Status
			}, "15s", "500ms").Should(BeElementOf(models.PlaybookRunStatusCompleted, models.PlaybookRunStatusFailed))
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

		useOn := func(conns ...models.Connection) func() {
			ginkgo.GinkgoHelper()
			grants := lo.Map(conns, func(c models.Connection, _ int) playbookGrant {
				return playbookGrant{
					Action:   adapter.ActionConnectionUse,
					Resource: v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: c.Name, Namespace: c.Namespace}}},
				}
			})
			return bindPlaybook(playbook, grants...)
		}

		create := func(conns ...*models.Connection) {
			ginkgo.GinkgoHelper()
			for _, c := range conns {
				c.ID, c.Source = uuid.New(), models.SourceUI
				Expect(DefaultContext.DB().Create(c).Error).To(Succeed())
				ginkgo.DeferCleanup(func() { Expect(DefaultContext.DB().Delete(c).Error).To(Succeed()) })
			}
		}

		ginkgo.It("checks the connections a connection's properties name in the namespace duty resolves them in", func() {
			callerAWS := models.Connection{Name: "caller-less-ns-aws", Namespace: "default", Type: models.ConnectionTypeAWS, Username: "caller-key", Password: "caller-secret"}
			ownAWS := models.Connection{Name: "caller-less-ns-aws", Namespace: "caller-less-other", Type: models.ConnectionTypeAWS, Username: "own-key", Password: "own-secret"}
			signed := models.Connection{
				Name: "caller-less-ns-signed", Namespace: "caller-less-other", Type: models.ConnectionTypeHTTP, URL: server.URL,
				Properties: types.JSONStringMap{"awsSigV4": `{"connection":"connection://caller-less-ns-aws","service":"es"}`},
			}
			create(&callerAWS, &ownAWS, &signed)
			ginkgo.DeferCleanup(useOn(signed, ownAWS))

			// duty fills in connection://caller-less-ns-aws from the namespace of the action, not of the HTTP connection
			_, err := (&actions.HTTP{}).Run(asPlaybook().WithNamespace("default"), v1.HTTPAction{
				HTTPConnection: connection.HTTPConnection{ConnectionName: "connection://caller-less-other/caller-less-ns-signed"},
			})
			Expect(err).To(MatchError(ContainSubstring("connection:use required on connection default/caller-less-ns-aws")))
		})

		ginkgo.It("checks the connections a property read from a configmap names", func() {
			k8s, err := DefaultContext.LocalKubernetes()
			Expect(err).To(Succeed())
			signing := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "caller-less-signing", Namespace: "default"},
				Data:       map[string]string{"awsSigV4": `{"connection":"connection://default/caller-less-cm-aws","service":"es"}`},
			}
			_, err = k8s.CoreV1().ConfigMaps(signing.Namespace).Create(DefaultContext, signing, metav1.CreateOptions{})
			Expect(err).To(Succeed())
			ginkgo.DeferCleanup(func() {
				Expect(k8s.CoreV1().ConfigMaps(signing.Namespace).Delete(DefaultContext, signing.Name, metav1.DeleteOptions{})).To(Succeed())
			})

			aws := models.Connection{Name: "caller-less-cm-aws", Namespace: "default", Type: models.ConnectionTypeAWS, Username: "access-key", Password: "secret-key"}
			signed := models.Connection{
				Name: "caller-less-cm-signed", Namespace: "default", Type: models.ConnectionTypeHTTP, URL: server.URL,
				Properties: types.JSONStringMap{"awsSigV4": "configmap://caller-less-signing/awsSigV4"},
			}
			create(&aws, &signed)
			ginkgo.DeferCleanup(useOn(signed))

			_, err = (&actions.HTTP{}).Run(asPlaybook(), v1.HTTPAction{
				HTTPConnection: connection.HTTPConnection{ConnectionName: "connection://default/caller-less-cm-signed"},
			})
			Expect(err).To(MatchError(ContainSubstring("connection:use required on connection default/caller-less-cm-aws")))

			_, err = rbac.GetConnectionForUse(asPlaybook(), "connection://default/caller-less-cm-signed")
			Expect(err).To(MatchError(ContainSubstring("connection:use required on connection default/caller-less-cm-aws")))
		})

		ginkgo.It("checks the connection duty has cached, not one that replaced it under the same name", func() {
			ref := "connection://default/caller-less-replaced"
			previous := models.Connection{
				ID: uuid.New(), Name: "caller-less-replaced", Namespace: "default", Source: models.SourceUI,
				Type: models.ConnectionTypeHTTP, URL: server.URL, Password: "previous-password",
			}
			Expect(DefaultContext.DB().Create(&previous).Error).To(Succeed())
			cached, err := DefaultContext.HydrateConnectionByURL(ref)
			Expect(err).To(Succeed())
			Expect(cached.ID).To(Equal(previous.ID))
			Expect(DefaultContext.DB().Delete(&previous).Error).To(Succeed())

			replacement := models.Connection{Name: previous.Name, Namespace: previous.Namespace, Type: models.ConnectionTypeHTTP, URL: server.URL, Password: "replacement-password"}
			create(&replacement)
			ginkgo.DeferCleanup(useOn(replacement))

			// Duty would fill in the previous connection's credentials for this reference until its cache expires
			err = rbac.AuthorizeConnectionRefUse(asPlaybook(), ref)
			Expect(err).To(MatchError(ContainSubstring("connection:use required on connection default/caller-less-replaced")))

			conn, err := rbac.GetConnectionForUse(asPlaybook(), ref)
			Expect(err).To(Succeed())
			Expect(conn.ID).To(Equal(replacement.ID))
			Expect(conn.Password).To(Equal("replacement-password"))
		})

		ginkgo.It("isn't checked again on an agent, for an action upstream dispatched to it", func() {
			// The agent has no binding for, and no record of, the upstream playbook
			upstreamPlaybookID := uuid.New()

			run := models.PlaybookRun{PlaybookID: playbook.ID, Status: models.PlaybookRunStatusRunning, Spec: playbook.Spec}
			Expect(DefaultContext.DB().Create(&run).Error).To(Succeed())
			action := models.PlaybookRunAction{
				Name: "fetch", PlaybookRunID: run.ID, Status: models.PlaybookActionStatusWaiting,
				ScheduledTime: time.Now().Add(-time.Second),
			}
			Expect(DefaultContext.DB().Create(&action).Error).To(Succeed())
			ginkgo.DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("action_id = ?", action.ID).Delete(&models.PlaybookActionAgentData{}).Error).To(Succeed())
				Expect(DefaultContext.DB().Delete(&action).Error).To(Succeed())
				Expect(DefaultContext.DB().Delete(&run).Error).To(Succeed())
			})

			spec, err := json.Marshal(v1.PlaybookAction{
				Name: "fetch",
				HTTP: &v1.HTTPAction{HTTPConnection: connection.HTTPConnection{ConnectionName: ref}},
			})
			Expect(err).To(Succeed())
			Expect(DefaultContext.DB().Create(&models.PlaybookActionAgentData{
				ActionID: action.ID, RunID: run.ID, PlaybookID: upstreamPlaybookID, Spec: spec, Env: []byte(`{}`),
			}).Error).To(Succeed())

			Eventually(func() models.PlaybookActionStatus {
				_, _ = ActionAgentConsumer(DefaultContext)
				var current models.PlaybookRunAction
				Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&current).Error).To(Succeed())
				return current.Status
			}, "10s", "200ms").Should(Equal(models.PlaybookActionStatusCompleted))

			var completed models.PlaybookRunAction
			Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&completed).Error).To(Succeed())
			Expect(completed.Result).To(HaveKeyWithValue("code", BeNumerically("==", http.StatusOK)), "the connection's credentials were filled in")
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
