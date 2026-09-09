package db

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func recoveryNotificationCRD() *v1.Notification {
	id := uuid.New()
	obj := &v1.Notification{ObjectMeta: metav1.ObjectMeta{Name: id.String(), Namespace: "default", UID: types.UID(id.String())}, Spec: v1.NotificationSpec{
		Events: []string{"config.unhealthy"}, To: v1.NotificationRecipientSpec{URL: api.SystemSMTP + "?To=ops@example.com"}, OnResolved: &v1.NotificationOnResolved{Enabled: true, WaitFor: "30s"},
	}}
	ginkgo.DeferCleanup(func() {
		Expect(DefaultContext.DB().Where("id = ?", id).Delete(&models.Notification{}).Error).To(Succeed())
	})
	return obj
}

func recoveryValidationConnection(kind string) models.Connection {
	conn := models.Connection{ID: uuid.New(), Name: uuid.NewString(), Namespace: "default", Type: kind, Source: models.SourceCRD}
	if kind == models.ConnectionTypeEmail {
		conn.URL = "smtp://localhost:2525/?From=alerts@example.com&To=ops@example.com"
	}
	Expect(DefaultContext.DB().Create(&conn).Error).To(Succeed())
	ginkgo.DeferCleanup(func() { Expect(DefaultContext.DB().Delete(&conn).Error).To(Succeed()) })
	return conn
}

var _ = ginkgo.Describe("Notification recovery persistence", func() {
	for _, kind := range []string{models.ConnectionTypeSlack, models.ConnectionTypeEmail, "system", "person"} {
		ginkgo.It("persists opt-in policy for "+kind, func() {
			obj := recoveryNotificationCRD()
			if kind == "person" {
				obj.Spec.To = v1.NotificationRecipientSpec{Person: dummy.JohnDoe.ID.String()}
			} else if kind != "system" {
				conn := recoveryValidationConnection(kind)
				obj.Spec.To = v1.NotificationRecipientSpec{Connection: conn.ID.String()}
			}
			obj.Spec.OnResolved.Template = "Recovered {{.resource.id}}"
			obj.Spec.OnResolved.Slack = &v1.NotificationResolvedSlack{Reply: lo.ToPtr(true), Reaction: "white_check_mark"}
			Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(Succeed())
			var stored models.Notification
			Expect(DefaultContext.DB().Where("id = ?", obj.UID).First(&stored).Error).To(Succeed())
			var actual v1.NotificationOnResolved
			Expect(json.Unmarshal(stored.OnResolved, &actual)).To(Succeed())
			Expect(actual).To(Equal(*obj.Spec.OnResolved))
			obj.Spec.OnResolved = nil
			Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(Succeed())
			stored = models.Notification{}
			Expect(DefaultContext.DB().Where("id = ?", obj.UID).First(&stored).Error).To(Succeed())
			Expect(stored.OnResolved).To(BeEmpty())
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*v1.NotificationSpec)
	}{
		{"grouped", func(s *v1.NotificationSpec) { s.GroupBy = []string{"type"} }},
		{"unsupported event", func(s *v1.NotificationSpec) { s.Events = []string{"incident.created"} }},
		{"generic Slack", func(s *v1.NotificationSpec) { s.To = v1.NotificationRecipientSpec{URL: "slack://token/channel"} }},
		{"playbook", func(s *v1.NotificationSpec) { s.To = v1.NotificationRecipientSpec{Playbook: lo.ToPtr("default/run")} }},
		{"negative stabilization", func(s *v1.NotificationSpec) { s.OnResolved.WaitFor = "-1s" }},
		{"missing connection", func(s *v1.NotificationSpec) { s.To = v1.NotificationRecipientSpec{Connection: uuid.NewString()} }},
	} {
		ginkgo.It("rejects "+tc.name+" before persistence", func() {
			obj := recoveryNotificationCRD()
			tc.change(&obj.Spec)
			Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(HaveOccurred())
			var count int64
			Expect(DefaultContext.DB().Model(&models.Notification{}).Where("id = ?", obj.UID).Count(&count).Error).To(Succeed())
			Expect(count).To(BeZero())
		})
	}
	ginkgo.It("validates mutable team and fallback transports and Slack operations", func() {
		obj := recoveryNotificationCRD()
		smtp := recoveryValidationConnection(models.ConnectionTypeEmail)
		slack := recoveryValidationConnection(models.ConnectionTypeSlack)
		unsupported := recoveryValidationConnection("webhook")
		team := models.Team{ID: uuid.New(), Name: uuid.NewString(), CreatedBy: dummy.JohnDoe.ID}
		set := func(conn models.Connection) {
			b, err := json.Marshal(api.TeamSpec{Notifications: []api.NotificationConfig{{Name: "ops", Connection: conn.ID.String()}}})
			Expect(err).NotTo(HaveOccurred())
			team.Spec = b
		}
		set(smtp)
		Expect(DefaultContext.DB().Create(&team).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Where("id = ?", obj.UID).Delete(&models.Notification{}).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(&team).Error).To(Succeed())
		})
		obj.Spec.To = v1.NotificationRecipientSpec{Team: team.ID.String()}
		Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(Succeed())
		set(unsupported)
		Expect(DefaultContext.DB().Model(&team).Update("spec", team.Spec).Error).To(Succeed())
		Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(HaveOccurred())
		set(slack)
		Expect(DefaultContext.DB().Model(&team).Update("spec", team.Spec).Error).To(Succeed())
		obj.Spec.OnResolved.Slack = &v1.NotificationResolvedSlack{Reply: lo.ToPtr(false)}
		Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(HaveOccurred())
		obj.Spec.OnResolved.Slack.Reaction = "white_check_mark"
		Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(Succeed())
		obj.Spec.Fallback = &v1.NotificationFallback{NotificationRecipientSpec: v1.NotificationRecipientSpec{Connection: unsupported.ID.String()}}
		Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(HaveOccurred())
	})
	ginkgo.It("leaves disabled policies on legacy transports valid", func() {
		obj := recoveryNotificationCRD()
		obj.Spec.OnResolved.Enabled = false
		obj.Spec.To = v1.NotificationRecipientSpec{URL: "slack://token/channel"}
		obj.Spec.Events = []string{"incident.created"}
		Expect(PersistNotificationFromCRD(DefaultContext, obj)).To(Succeed())
	})
})
