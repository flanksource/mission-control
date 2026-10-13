package permissions_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/rbactest"
)

var _ = Describe("Sending a notification through a connection", Ordered, func() {
	const namespace = "notification-send-test"

	var (
		conn     models.Connection
		config   models.ConfigItem
		notif    models.Notification
		server   *httptest.Server
		received atomic.Int32
		ref      string
	)

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	payload := func() notification.NotificationEventPayload {
		return notification.NotificationEventPayload{
			EventID:        config.ID,
			EventName:      api.EventConfigUnhealthy,
			ResourceID:     config.ID,
			NotificationID: notif.ID,
			EventCreatedAt: time.Now(),
			CustomService:  &api.NotificationConfig{Connection: ref},
		}
	}

	latestHistory := func() models.NotificationSendHistory {
		GinkgoHelper()
		var history models.NotificationSendHistory
		Expect(DefaultContext.DB().Where("notification_id = ?", notif.ID).Order("created_at DESC").First(&history).Error).To(Succeed())
		return history
	}

	BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			received.Add(1)
			w.WriteHeader(http.StatusOK)
		}))

		conn = models.Connection{
			ID:         uuid.New(),
			Name:       "notification-webhook",
			Namespace:  namespace,
			Source:     models.SourceUI,
			Type:       models.ConnectionTypeHTTP,
			URL:        fmt.Sprintf("generic+%s/hook", server.URL),
			Properties: types.JSONStringMap{"disabletls": "yes", "template": "json"},
		}
		Expect(DefaultContext.DB().Create(&conn).Error).To(Succeed())
		ref = fmt.Sprintf("connection://%s/%s", namespace, conn.Name)

		config = models.ConfigItem{
			ID:          uuid.New(),
			Name:        lo.ToPtr("notification-send-target"),
			ConfigClass: models.ConfigClassPod,
			Type:        lo.ToPtr("Kubernetes::Pod"),
			Health:      lo.ToPtr(models.HealthUnhealthy),
		}
		Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())

		notif = models.Notification{
			ID:        uuid.New(),
			Name:      "send-through-connection",
			Namespace: namespace,
			Events:    []string{api.EventConfigUnhealthy},
			Source:    models.SourceUI,
		}
		Expect(DefaultContext.DB().Create(&notif).Error).To(Succeed())

		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: "webhook-connection", Namespace: namespace, UID: uid()},
			Spec: v1.ScopeSpec{Targets: []v1.ScopeTarget{{Connection: &v1.ScopeConnectionSelector{
				ScopeResourceRef: v1.ScopeResourceRef{Name: conn.Name, Namespace: namespace},
			}}}},
		})).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	AfterAll(func() {
		server.Close()
		notification.PurgeCache(notif.ID.String())
		Expect(DefaultContext.DB().Where("notification_id = ?", notif.ID).Delete(&models.NotificationSendHistory{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&notif).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&config).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&conn).Error).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	It("records the connection to send through by id when it's evaluated, without a check or its credentials", func() {
		// Filling in this connection's credentials fails: there's no such secret
		unresolvable := models.Connection{
			ID: uuid.New(), Name: "unresolvable", Namespace: namespace, Source: models.SourceUI,
			Type: models.ConnectionTypeHTTP, URL: server.URL, Password: "secret://missing/password",
		}
		Expect(DefaultContext.DB().Create(&unresolvable).Error).To(Succeed())
		DeferCleanup(func() { Expect(DefaultContext.DB().Delete(&unresolvable).Error).To(Succeed()) })
		_, err := DefaultContext.HydrateConnection(&unresolvable)
		Expect(err).To(HaveOccurred())

		event := models.Event{Name: api.EventConfigUnhealthy, EventID: config.ID}
		celEnv, err := notification.GetEnvForEvent(DefaultContext, event)
		Expect(err).To(Succeed())

		payloads, err := notification.CreateNotificationSendPayloads(DefaultContext, event, &notification.NotificationWithSpec{
			Notification:        notif,
			CustomNotifications: []api.NotificationConfig{{Connection: fmt.Sprintf("connection://%s/%s", namespace, unresolvable.Name)}},
		}, celEnv)
		Expect(err).To(Succeed())
		Expect(payloads).To(HaveLen(1))
		Expect(payloads[0].Connection).To(Equal(&unresolvable.ID))
	})

	It("fails without connection:use, and records why on the send history and the notification", func() {
		Expect(notification.SendEventPayload(DefaultContext, payload())).To(MatchError(ContainSubstring("connection:use required")))

		history := latestHistory()
		Expect(history.Status).To(Equal(models.NotificationStatusError))
		Expect(lo.FromPtr(history.Error)).To(ContainSubstring("connection:use required on connection %s/%s", namespace, conn.Name))

		var stored models.Notification
		Expect(DefaultContext.DB().Where("id = ?", notif.ID).First(&stored).Error).To(Succeed())
		Expect(lo.FromPtr(stored.Error)).To(ContainSubstring("connection:use required on connection %s/%s", namespace, conn.Name))

		Expect(received.Load()).To(BeZero())
	})

	It("is sent with connection:use on the connection, as the notification", func() {
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "send-through-webhook", Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{{
				Name: "use-webhook", Action: adapter.ActionConnectionUse, Resource: &v1.ScopeReference{ScopeRef: "webhook-connection"},
			}}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "send-through-webhook", Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{Role: "send-through-webhook", Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{
					Notifications: []v1.PermissionGroupSelector{{Name: notif.Name, Namespace: namespace}},
				},
			}},
		})).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())

		var system models.Person
		Expect(DefaultContext.DB().Where("name = 'System'").First(&system).Error).To(Succeed())

		recorder, stop := rbactest.RecordSubjects()
		DeferCleanup(stop)

		// As "mission-control notification send" does, with no person
		Expect(notification.SendEventPayload(DefaultContext, payload())).To(Succeed())

		Expect(recorder.Subjects()).ToNot(BeEmpty())
		Expect(recorder.Subjects()).ToNot(ContainElement(system.ID.String()))
		Expect(recorder.Subjects()).To(HaveEach(notif.ID.String()))

		history := latestHistory()
		Expect(history.Status).To(Equal(models.NotificationStatusSent), lo.FromPtr(history.Error))
		Expect(history.ConnectionID).To(Equal(&conn.ID))
		Expect(received.Load()).To(BeEquivalentTo(1))
	})
})
