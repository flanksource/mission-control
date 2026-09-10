//go:build recoverytests

package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/casbin/casbin/v2/persist"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/emersion/go-smtp"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"github.com/slack-go/slack"
)

type recoveryMail struct {
	From string
	To   []string
	Data []byte
}
type recoverySMTP struct {
	mu       sync.Mutex
	messages []recoveryMail
	fail     bool
}
type recoverySMTPSession struct {
	backend *recoverySMTP
	message recoveryMail
}

func (b *recoverySMTP) NewSession(*smtp.Conn) (smtp.Session, error) {
	return &recoverySMTPSession{backend: b}, nil
}
func (s *recoverySMTPSession) Mail(from string, _ *smtp.MailOptions) error {
	s.message.From = from
	return nil
}
func (s *recoverySMTPSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.message.To = append(s.message.To, to)
	return nil
}
func (s *recoverySMTPSession) Reset()        { s.message = recoveryMail{} }
func (s *recoverySMTPSession) Logout() error { return nil }
func (s *recoverySMTPSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.message.Data = b
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	if s.backend.fail {
		return fmt.Errorf("mock SMTP failure")
	}
	s.backend.messages = append(s.backend.messages, s.message)
	return nil
}
func (b *recoverySMTP) read() []recoveryMail {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]recoveryMail(nil), b.messages...)
}
func newRecoverySMTP(n NotificationWithSpec) (*recoverySMTP, models.Connection) {
	backend := &recoverySMTP{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	server := smtp.NewServer(backend)
	server.Domain = "localhost"
	go func() { _ = server.Serve(listener) }()
	ginkgo.DeferCleanup(func() { Expect(server.Close()).To(Succeed()) })
	conn := (v1.ConnectionSMTP{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Auth: v1.SMTPAuthNone, Encryption: v1.EncryptionNone, FromAddress: "original@example.com", FromName: "Original Sender", ToAddresses: []string{"one@example.com", "two@example.com"}}).ToModel()
	conn.ID = uuid.New()
	conn.Name = conn.ID.String()
	conn.Source = models.SourceCRD
	Expect(setup.DefaultContext.DB().Create(&conn).Error).To(Succeed())
	ginkgo.DeferCleanup(func() { Expect(setup.DefaultContext.DB().Delete(&conn).Error).To(Succeed()) })
	grantRecoveryConnection(n, conn)
	return backend, conn
}

type recoveryPolicyAdapter struct{ *stringadapter.Adapter }

func (*recoveryPolicyAdapter) AddPolicy(string, string, []string) error                  { return nil }
func (*recoveryPolicyAdapter) RemovePolicy(string, string, []string) error               { return nil }
func (*recoveryPolicyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error { return nil }

func (*recoveryPolicyAdapter) AddPolicies(string, string, [][]string) error    { return nil }
func (*recoveryPolicyAdapter) RemovePolicies(string, string, [][]string) error { return nil }

func grantRecoveryConnection(n NotificationWithSpec, conn models.Connection) {
	_, err := rbac.Enforcer().AddPolicy(n.ID.String(), "*", policy.ActionRead, "allow", fmt.Sprintf("r.obj.Connection.Name == %q", conn.Name), conn.ID.String())
	Expect(err).NotTo(HaveOccurred())
}

var recoveryRBAC sync.Once

func newRecoveryFixture() (NotificationWithSpec, models.ConfigItem, NotificationEventPayload) {
	recoveryRBAC.Do(func() {
		Expect(rbac.Init(setup.DefaultContext, []string{dummy.JohnDoe.ID.String()}, func(context.Context, *gormadapter.Adapter) persist.Adapter {
			return &recoveryPolicyAdapter{stringadapter.NewAdapter("# isolated notification recovery test policies")}
		})).To(Succeed())
		rbac.Stop()
	})
	n := NotificationWithSpec{Notification: models.Notification{ID: uuid.New(), Name: uuid.NewString(), Events: []string{}, Source: models.SourceCRD, OnResolved: types.JSON(`{"enabled":true}`)}, OnResolved: &v1.NotificationOnResolved{Enabled: true}}
	Expect(setup.DefaultContext.DB().Create(&n.Notification).Error).To(Succeed())

	config := models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("recovery-service"), Type: lo.ToPtr("Test::Recovery"), Health: lo.ToPtr(models.HealthUnhealthy)}
	Expect(setup.DefaultContext.DB().Create(&config).Error).To(Succeed())
	ginkgo.DeferCleanup(func() {
		_, err := rbac.Enforcer().RemoveFilteredPolicy(0, n.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(rbac.DeleteAllRolesForUser(n.ID.String())).To(Succeed())
		Expect(setup.DefaultContext.DB().Delete(&n.Notification).Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Delete(&config).Error).To(Succeed())
		PurgeCache(n.ID.String())
	})
	state, err := currentRecoveryHealth(setup.DefaultContext, "config", config.ID)
	Expect(err).NotTo(HaveOccurred())
	payload := NotificationEventPayload{NotificationID: n.ID, ResourceID: config.ID, EventID: config.ID, EventName: "config.unhealthy", EventCreatedAt: time.Now(), RecoveryEpisode: state.EpisodeID.String()}
	return n, config, payload
}
func recoveryContext(n *NotificationWithSpec, payload NotificationEventPayload) *Context {
	ctx := NewContext(setup.DefaultContext.WithSubject(n.ID.String()), n.ID)
	ctx.WithSource(payload.EventName, payload.ResourceID)
	ctx.log.Payload = payload.AsMap()
	Expect(ctx.StartLog()).To(Succeed())
	skip, err := prepareRecoveryDispatch(ctx, n, payload)
	Expect(err).NotTo(HaveOccurred())
	Expect(skip).To(BeFalse())
	return ctx
}
func readyRecoveries(n NotificationWithSpec) {
	for _, receipt := range loadRecoveryReceipts(n) {
		Expect(wakeNotificationDelivery(setup.DefaultContext, receipt.ID)).To(Succeed())
	}
	Expect(setup.DefaultContext.DB().Model(&models.NotificationDelivery{}).Where("notification_id = ?", n.ID).Update("not_before", time.Now().Add(-time.Hour)).Error).To(Succeed())
}
func loadRecoveryReceipts(n NotificationWithSpec) []models.NotificationDelivery {
	var rows []models.NotificationDelivery
	Expect(setup.DefaultContext.DB().Where("notification_id = ?", n.ID).Order("created_at").Find(&rows).Error).To(Succeed())
	return rows
}

var _ = ginkgo.Describe("Notification recovery", func() {
	for _, raw := range []bool{true, false} {
		ginkgo.It(fmt.Sprintf("threads SMTP raw=%t with original envelope, headers, From and subject after connection changes", raw), func() {
			n, config, payload := newRecoveryFixture()
			backend, conn := newRecoverySMTP(n)
			ctx := recoveryContext(&n, payload)
			var err error
			if raw {
				_, err = SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Title: "Original subject", Message: "Original unhealthy"}, &n)
			} else {
				_, err = SendNotification(ctx, conn.ID.String(), "", NotificationMessagePayload{Title: "Original subject", Description: "Original unhealthy"}, nil, nil)
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(backend.read()).To(HaveLen(1))
			original, err := netmail.ReadMessage(bytes.NewReader(backend.read()[0].Data))
			Expect(err).NotTo(HaveOccurred())
			Expect(original.Header.Get("Message-ID")).NotTo(BeEmpty())
			Expect(original.Header.Get("In-Reply-To")).To(BeEmpty())
			Expect(loadRecoveryReceipts(n)).To(HaveLen(1))
			Expect(setup.DefaultContext.DB().Model(&conn).Update("properties", types.JSONStringMap{"from": "changed@example.com", "to": "changed@example.com"}).Error).To(Succeed())
			Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
			readyRecoveries(n)
			Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
			messages := backend.read()
			Expect(messages).To(HaveLen(2))
			recovered, err := netmail.ReadMessage(bytes.NewReader(messages[1].Data))
			Expect(err).NotTo(HaveOccurred())
			Expect(messages[1].To).To(Equal(messages[0].To))
			Expect(messages[1].From).To(Equal(messages[0].From))
			Expect(recovered.Header.Get("From")).To(Equal(original.Header.Get("From")))
			Expect(recovered.Header.Get("Subject")).To(Equal("Re: Original subject"))
			Expect(recovered.Header.Get("In-Reply-To")).To(Equal(original.Header.Get("Message-ID")))
			Expect(recovered.Header.Get("References")).To(Equal(original.Header.Get("Message-ID")))
			Expect(recovered.Header.Get("Message-ID")).NotTo(Equal(original.Header.Get("Message-ID")))
			Expect(string(messages[1].Data)).To(ContainSubstring("Resolved"))
			Expect(string(messages[1].Data)).To(ContainSubstring("recovery-service"))
			readyRecoveries(n)
			Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
			Expect(backend.read()).To(HaveLen(2))
		})
	}
	ginkgo.It("skips recovered-before-send and obsolete episodes without orphan receipts", func() {
		n, config, payload := newRecoveryFixture()
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		ctx := NewContext(setup.DefaultContext, n.ID)
		skip, err := prepareRecoveryDispatch(ctx, &n, payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(skip).To(BeTrue())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "unhealthy").Error).To(Succeed())
		skip, err = prepareRecoveryDispatch(ctx, &n, payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(skip).To(BeTrue())
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})
	ginkgo.It("defers both distinct episodes until the latest healthy transition is stable", func() {
		n, config, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		n.OnResolved.WaitFor = "1h"
		first := recoveryContext(&n, payload)
		_, err := SendRawNotification(first, conn.ID.String(), "", nil, NotificationTemplate{Title: "outage A", Message: "unhealthy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "warning").Error).To(Succeed())
		state, err := currentRecoveryHealth(setup.DefaultContext, "config", config.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state.EpisodeID.String()).To(Equal(payload.RecoveryEpisode))
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(backend.read()).To(HaveLen(1))
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "unhealthy").Error).To(Succeed())
		state, err = currentRecoveryHealth(setup.DefaultContext, "config", config.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state.EpisodeID.String()).NotTo(Equal(payload.RecoveryEpisode))
		payload.RecoveryEpisode = state.EpisodeID.String()
		second := recoveryContext(&n, payload)
		_, err = SendRawNotification(second, conn.ID.String(), "", nil, NotificationTemplate{Title: "outage B", Message: "unhealthy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(backend.read()).To(HaveLen(2))
		Expect(setup.DefaultContext.DB().Model(&models.NotificationHealthState{}).Where("resource_id = ?", config.ID).Update("healthy_since", time.Now().Add(-2*time.Hour)).Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(backend.read()).To(HaveLen(4))
		for _, r := range loadRecoveryReceipts(n) {
			Expect(r.ResolvedAt).NotTo(BeNil())
		}
	})
	ginkgo.It("reconciles healthy-before-receipt-commit and preserves receipts after history cleanup", func() {
		n, config, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		ctx := recoveryContext(&n, payload)
		Expect(setRecoveryTransport(ctx, &conn, "")).To(Succeed())
		receipt, err := beginDelivery(ctx, "smtp", recoveryDestination{To: []string{"delivered@example.com"}, From: "original@example.com", Subject: "original"})
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(backend.read()).To(BeEmpty())
		Expect(finishDelivery(ctx, receipt, "", "", nil)).To(Succeed())
		Expect(backend.read()).To(BeEmpty())
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(backend.read()).To(HaveLen(1))
		Expect(setup.DefaultContext.DB().Delete(ctx.log).Error).To(Succeed())
		Expect(loadRecoveryReceipts(n)[0].HistoryID).To(BeNil())
		Expect(loadRecoveryReceipts(n)[0].ResolvedAt).NotTo(BeNil())
	})
	ginkgo.It("rejects unsupported mutable transport and reserved SMTP headers without sending", func() {
		n, _, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		ctx := recoveryContext(&n, payload)
		Expect(setRecoveryTransport(ctx, nil, "slack://secret/channel")).To(HaveOccurred())
		Expect(setRecoveryTransport(ctx, &models.Connection{Type: "webhook"}, "")).To(HaveOccurred())
		_, err := SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Title: "test", Message: "test", Properties: map[string]string{"headers": "Message-ID=override"}}, &n)
		Expect(err).To(HaveOccurred())
		Expect(backend.read()).To(BeEmpty())
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})
	ginkgo.It("keeps disabled sends unthreaded and without receipts", func() {
		n, _, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		n.OnResolved.Enabled = false
		ctx := NewContext(setup.DefaultContext.WithSubject(n.ID.String()), n.ID)
		ctx.WithSource(payload.EventName, payload.ResourceID)
		skip, err := prepareRecoveryDispatch(ctx, &n, payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(skip).To(BeFalse())
		_, err = SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Title: "legacy", Message: "legacy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		Expect(backend.read()).To(HaveLen(1))
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})
	for _, kind := range []string{"config", "component", "check"} {
		ginkgo.It("provides normal "+kind+" template environment with recovery metadata", func() {
			n, config, _ := newRecoveryFixture()
			_ = n
			id := config.ID
			if kind == "component" {
				id = dummy.Logistics.ID
			} else if kind == "check" {
				id = uuid.UUID(dummy.LogisticsAPIHealthHTTPCheck.ID)
			}
			now := time.Now()
			env, err := recoveryEnvironment(setup.DefaultContext, models.NotificationHealthEpisode{ResourceType: kind, ResourceID: id}, models.NotificationHealthState{Health: "healthy", HealthySince: &now})
			Expect(err).NotTo(HaveOccurred())
			values := env.AsMap(setup.DefaultContext)
			resource, ok := values[kind].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(resource["name"]).NotTo(BeEmpty())
			Expect(values["source_event"]).To(Equal(lo.Ternary(kind == "check", "check.passed", kind+".healthy")))
			Expect(env.Permalink).NotTo(BeEmpty())
		})
	}
	ginkgo.It("materializes fallback team rules idempotently and retries only failed destinations", func() {
		n, _, payload := newRecoveryFixture()
		good, goodConn := newRecoverySMTP(n)
		bad, badConn := newRecoverySMTP(n)
		bad.fail = true
		spec := api.TeamSpec{Notifications: []api.NotificationConfig{{Name: "different-a", Connection: goodConn.ID.String()}, {Name: "different-b", Connection: badConn.ID.String()}}}
		encoded, err := json.Marshal(spec)
		Expect(err).NotTo(HaveOccurred())
		team := models.Team{ID: uuid.New(), Name: uuid.NewString(), Spec: encoded, CreatedBy: dummy.JohnDoe.ID}
		Expect(setup.DefaultContext.DB().Create(&team).Error).To(Succeed())
		ginkgo.DeferCleanup(func() { Expect(setup.DefaultContext.DB().Delete(&team).Error).To(Succeed()) })
		n.FallbackTeamID = &team.ID
		Expect(setup.DefaultContext.DB().Model(&n.Notification).Update("fallback_team_id", team.ID).Error).To(Succeed())
		payload.PersonID = &dummy.JohnDoe.ID
		payload.NotificationName = "original-rule"
		parent := recoveryContext(&n, payload)
		Expect(materializeRecoveryFallback(setup.DefaultContext, &n, *parent.log)).To(Succeed())
		Expect(materializeRecoveryFallback(setup.DefaultContext, &n, *parent.log)).To(Succeed())
		var attempts []models.NotificationSendHistory
		Expect(setup.DefaultContext.DB().Where("parent_id = ?", parent.log.ID).Find(&attempts).Error).To(Succeed())
		Expect(attempts).To(HaveLen(2))
		for _, attempt := range attempts {
			var p NotificationEventPayload
			p.FromMap(attempt.Payload)
			Expect(p.PersonID).To(BeNil())
			Expect(p.TeamID).To(Equal(&team.ID))
			Expect(p.RecoveryEpisode).To(Equal(payload.RecoveryEpisode))
			Expect(p.NotificationName).To(HavePrefix("different-"))
		}
		for range 2 {
			_, _ = processRecoveryPending(setup.DefaultContext, true)
		}
		Expect(good.read()).To(HaveLen(1))
		Expect(bad.read()).To(BeEmpty())
		bad.mu.Lock()
		bad.fail = false
		bad.mu.Unlock()
		Expect(setup.DefaultContext.DB().Model(&models.NotificationSendHistory{}).Where("parent_id = ? AND status != 'sent'", parent.log.ID).Update("not_before", time.Now().Add(-time.Hour)).Error).To(Succeed())
		handled, err := processRecoveryPending(setup.DefaultContext, true)
		Expect(handled).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())
		Expect(good.read()).To(HaveLen(1))
		Expect(bad.read()).To(HaveLen(1))
		receipts := loadRecoveryReceipts(n)
		Expect(receipts).To(HaveLen(2))
		for _, receipt := range receipts {
			Expect(receipt.SentAt).NotTo(BeNil())
		}
	})
	ginkgo.It("resets repeat suppression on genuine recovery but not warning/unhealthy transitions", func() {
		n, config, payload := newRecoveryFixture()
		_, conn := newRecoverySMTP(n)
		n.RepeatInterval = lo.ToPtr(time.Hour)
		grantRecoveryConnection(n, conn)
		ctx := recoveryContext(&n, payload)
		_, err := SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Message: "unhealthy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		ctx.log.Sent()
		Expect(ctx.EndLog()).To(Succeed())
		prior, err := checkRepeatInterval(setup.DefaultContext, n, nil, config.ID.String(), payload.EventName)
		Expect(err).NotTo(HaveOccurred())
		Expect(prior).NotTo(BeNil())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "warning").Error).To(Succeed())
		prior, err = checkRepeatInterval(setup.DefaultContext, n, nil, config.ID.String(), payload.EventName)
		Expect(err).NotTo(HaveOccurred())
		Expect(prior).NotTo(BeNil())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "unhealthy").Error).To(Succeed())
		prior, err = checkRepeatInterval(setup.DefaultContext, n, nil, config.ID.String(), payload.EventName)
		Expect(err).NotTo(HaveOccurred())
		Expect(prior).To(BeNil())
	})
	ginkgo.It("leases pending recovered alerts without sending or inventing receipts", func() {
		n, config, payload := newRecoveryFixture()
		payload.CustomService = &api.NotificationConfig{URL: api.SystemSMTP + "?To=never@example.com"}
		history := models.NotificationSendHistory{ID: uuid.New(), NotificationID: n.ID, ResourceID: config.ID, SourceEvent: payload.EventName, Payload: payload.AsMap(), Status: models.NotificationStatusPending, NotBefore: lo.ToPtr(time.Now().Add(-time.Minute))}
		Expect(setup.DefaultContext.DB().Create(&history).Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		handled, err := processRecoveryPending(setup.DefaultContext, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		var actual models.NotificationSendHistory
		Expect(setup.DefaultContext.DB().Where("id = ?", history.ID).First(&actual).Error).To(Succeed())
		Expect(actual.Status).To(Equal(models.NotificationStatusSkipped))
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})
	ginkgo.It("records exact Slack reply/reaction targets and retries only a failed reaction", func() {
		n, config, payload := newRecoveryFixture()
		n.OnResolved.Slack = &v1.NotificationResolvedSlack{Reaction: "white_check_mark"}
		var mu sync.Mutex
		var calls []url.Values
		reactionFailures := 1
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, r.Form)
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(r.URL.Path, "reactions.add") && reactionFailures > 0 {
				reactionFailures--
				_, _ = io.WriteString(w, `{"ok":false,"error":"ratelimited"}`)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true,"channel":"Cactual","ts":"123.456"}`)
		}))
		ginkgo.DeferCleanup(server.Close)
		old := newSlackClient
		newSlackClient = func(token string) *slack.Client { return slack.New(token, slack.OptionAPIURL(server.URL+"/")) }
		ginkgo.DeferCleanup(func() { newSlackClient = old })
		conn := models.Connection{ID: uuid.New(), Name: uuid.NewString(), Type: models.ConnectionTypeSlack, Username: "Coriginal", Password: "mock-token", Source: models.SourceCRD}
		Expect(setup.DefaultContext.DB().Create(&conn).Error).To(Succeed())
		ginkgo.DeferCleanup(func() { Expect(setup.DefaultContext.DB().Delete(&conn).Error).To(Succeed()) })
		grantRecoveryConnection(n, conn)
		ctx := recoveryContext(&n, payload)
		_, err := SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Message: "unhealthy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		Expect(loadRecoveryReceipts(n)[0].Channel).To(Equal("Cactual"))
		Expect(loadRecoveryReceipts(n)[0].MessageID).To(Equal("123.456"))
		Expect(setup.DefaultContext.DB().Model(&conn).Update("username", "Cchanged").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(HaveOccurred())
		receipt := loadRecoveryReceipts(n)[0]
		Expect(receipt.ReplyAt).NotTo(BeNil())
		Expect(receipt.ReactionAt).To(BeNil())
		_, err = rbac.Enforcer().RemoveFilteredPolicy(0, n.ID.String())
		Expect(err).NotTo(HaveOccurred())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(HaveOccurred())
		Expect(loadRecoveryReceipts(n)[0].ReplyAt).To(Equal(receipt.ReplyAt))
		mu.Lock()
		Expect(calls).To(HaveLen(3))
		mu.Unlock()
		grantRecoveryConnection(n, conn)
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		mu.Lock()
		defer mu.Unlock()
		Expect(calls).To(HaveLen(4))
		Expect(calls[1].Get("channel")).To(Equal("Cactual"))
		Expect(calls[1].Get("thread_ts")).To(Equal("123.456"))
		for _, call := range calls[2:] {
			Expect(call.Get("channel")).To(Equal("Cactual"))
			Expect(call.Get("timestamp")).To(Equal("123.456"))
			Expect(call.Get("name")).To(Equal("white_check_mark"))
		}
	})
})
