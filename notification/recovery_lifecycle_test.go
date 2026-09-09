//go:build recoverytests

package notification

import (
	"encoding/json"
	"github.com/flanksource/incident-commander/mail"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Notification recovery lifecycle", func() {
	for _, kind := range []string{"config", "component", "check"} {
		ginkgo.It("rejects reordered and coalesced "+kind+" source events and recovers from persisted state", func() {
			n, config, _ := newRecoveryFixture()
			backend, conn := newRecoverySMTP(n)
			id, table, key, column, eventName := config.ID, "config_items", "id", "health", "config.unhealthy"
			if kind == "component" {
				id, table, eventName = dummy.Logistics.ID, "components", "component.unhealthy"
			}
			if kind == "check" {
				id, table, key, column, eventName = uuid.UUID(dummy.LogisticsAPIHealthHTTPCheck.ID), "checks_unlogged", "check_id", "status", "check.failed"
				created := setup.DefaultContext.DB().Exec("INSERT INTO checks_unlogged(check_id, canary_id, status) SELECT id, canary_id, 'healthy' FROM checks WHERE id = ? ON CONFLICT DO NOTHING", id)
				Expect(created.Error).NotTo(HaveOccurred())
				if created.RowsAffected == 1 {
					ginkgo.DeferCleanup(func() {
						Expect(setup.DefaultContext.DB().Exec("DELETE FROM checks_unlogged WHERE check_id = ?", id).Error).To(Succeed())
					})
				}
			}
			var prior string
			Expect(setup.DefaultContext.DB().Table(table).Select(column).Where(key+" = ?", id).Scan(&prior).Error).To(Succeed())
			ginkgo.DeferCleanup(func() {
				Expect(setup.DefaultContext.DB().Table(table).Where(key+" = ?", id).Update(column, prior).Error).To(Succeed())
			})
			update := func(health string) {
				if kind == "check" {
					Expect(setup.DefaultContext.DB().Exec("UPDATE checks_unlogged SET status = ?, last_runtime = (SELECT max(time) FROM check_statuses WHERE check_id = ? AND status = ?) WHERE check_id = ?", health, id, health == "healthy", id).Error).To(Succeed())
				} else {
					Expect(setup.DefaultContext.DB().Table(table).Where(key+" = ?", id).Update(column, health).Error).To(Succeed())
				}
			}
			source := func() models.Event {
				var e models.Event
				Expect(setup.DefaultContext.DB().Where("name = ? AND event_id = ?", eventName, id).First(&e).Error).To(Succeed())
				return e
			}
			update("healthy")
			update("unhealthy")
			old := source()
			n.CustomNotifications = []api.NotificationConfig{{Connection: conn.ID.String()}}
			env, err := GetEnvForEvent(setup.DefaultContext, old)
			Expect(err).NotTo(HaveOccurred())
			first, err := CreateNotificationSendPayloads(setup.DefaultContext.WithSubject(n.ID.String()), old, &n, env)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(HaveLen(1))
			ctx := recoveryContext(&n, first[0])
			_, err = SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Title: kind + " unhealthy", Message: "unhealthy"}, &n)
			Expect(err).NotTo(HaveOccurred())
			update("healthy")
			update("unhealthy")
			obsolete, err := CreateNotificationSendPayloads(setup.DefaultContext.WithSubject(n.ID.String()), old, &n, env)
			Expect(err).NotTo(HaveOccurred())
			Expect(obsolete).To(BeEmpty())
			current := source()
			Expect(current.Properties["recovery_episode"]).NotTo(Equal(old.Properties["recovery_episode"]))
			latest, err := CreateNotificationSendPayloads(setup.DefaultContext.WithSubject(n.ID.String()), current, &n, env)
			Expect(err).NotTo(HaveOccurred())
			Expect(latest).To(HaveLen(1))
			ctx = recoveryContext(&n, latest[0])
			_, err = SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Title: kind + " second outage", Message: "unhealthy"}, &n)
			Expect(err).NotTo(HaveOccurred())
			update("unknown")
			readyRecoveries(n)
			Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
			Expect(backend.read()).To(HaveLen(2))
			update("healthy")
			PurgeCache(n.ID.String())
			readyRecoveries(n)
			Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
			Expect(backend.read()).To(HaveLen(4))
			receipts := loadRecoveryReceipts(n)
			Expect(receipts).To(HaveLen(2))
			Expect(receipts[0].EpisodeID).NotTo(Equal(receipts[1].EpisodeID))
			for _, receipt := range receipts {
				Expect(receipt.ResolvedAt).NotTo(BeNil())
			}
		})
	}
	for _, nextTransport := range []string{"smtp", "slack", "removed", "renamed", "webhook"} {
		ginkgo.It("binds failed team-send retries to the original intent after routing changes to "+nextTransport, func() {
			n, config, payload := newRecoveryFixture()
			backend, original := newRecoverySMTP(n)
			backend.fail = true
			otherBackend, other := newRecoverySMTP(n)
			if nextTransport == "slack" {
				Expect(setup.DefaultContext.DB().Model(&other).Update("type", models.ConnectionTypeSlack).Error).To(Succeed())
			}
			team := models.Team{ID: uuid.New(), Name: uuid.NewString(), CreatedBy: dummy.JohnDoe.ID}
			setRule := func(conn models.Connection) {
				b, err := json.Marshal(api.TeamSpec{Notifications: []api.NotificationConfig{{Name: "route", Connection: conn.ID.String()}}})
				Expect(err).NotTo(HaveOccurred())
				team.Spec = b
			}
			setRule(original)
			Expect(setup.DefaultContext.DB().Create(&team).Error).To(Succeed())
			ginkgo.DeferCleanup(func() { Expect(setup.DefaultContext.DB().Delete(&team).Error).To(Succeed()) })
			payload.TeamID, payload.NotificationName = &team.ID, "route"
			ctx := recoveryContext(&n, payload)
			Expect(sendPendingNotification(setup.DefaultContext, *ctx.log, payload)).To(HaveOccurred())
			before := loadRecoveryReceipts(n)[0]
			Expect(before.ConnectionID).To(Equal(&original.ID))
			Expect(before.SentAt).To(BeNil())
			setRule(other)
			if nextTransport == "removed" || nextTransport == "renamed" || nextTransport == "webhook" {
				replacement := api.TeamSpec{}
				if nextTransport == "renamed" {
					replacement.Notifications = []api.NotificationConfig{{Name: "renamed", Connection: other.ID.String()}}
				}
				if nextTransport == "webhook" {
					replacement.Notifications = []api.NotificationConfig{{Name: "route", Webhook: &api.NotificationWebhookReceiver{}}}
				}
				encoded, err := json.Marshal(replacement)
				Expect(err).NotTo(HaveOccurred())
				team.Spec = encoded
			}
			Expect(setup.DefaultContext.DB().Model(&team).Update("spec", team.Spec).Error).To(Succeed())
			backend.mu.Lock()
			backend.fail = false
			backend.mu.Unlock()
			_, err := rbac.Enforcer().RemoveFilteredPolicy(0, n.ID.String())
			Expect(err).NotTo(HaveOccurred())
			grantRecoveryConnection(n, other)
			Expect(sendPendingNotification(setup.DefaultContext, *ctx.log, payload)).To(HaveOccurred())
			Expect(backend.read()).To(BeEmpty())
			Expect(otherBackend.read()).To(BeEmpty())
			grantRecoveryConnection(n, original)
			Expect(sendPendingNotification(setup.DefaultContext, *ctx.log, payload)).To(Succeed())
			after := loadRecoveryReceipts(n)[0]
			Expect(after.ConnectionID).To(Equal(&original.ID))
			Expect(after.MessageID).To(Equal(before.MessageID))
			Expect(after.SentAt).NotTo(BeNil())
			Expect(backend.read()).To(HaveLen(1))
			Expect(otherBackend.read()).To(BeEmpty())
			Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
			readyRecoveries(n)
			Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
			Expect(backend.read()).To(HaveLen(2))
			Expect(otherBackend.read()).To(BeEmpty())
		})
	}
	ginkgo.It("denies an ungranted named connection and rechecks revoked permission before SMTP recovery", func() {
		n, config, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		_, err := rbac.Enforcer().RemoveFilteredPolicy(0, n.ID.String())
		Expect(err).NotTo(HaveOccurred())
		ctx := recoveryContext(&n, payload)
		_, err = SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Message: "unhealthy"}, &n)
		Expect(err).To(HaveOccurred())
		Expect(backend.read()).To(BeEmpty())
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
		grantRecoveryConnection(n, conn)
		_, err = SendRawNotification(ctx, conn.ID.String(), "", nil, NotificationTemplate{Message: "unhealthy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		_, err = rbac.Enforcer().RemoveFilteredPolicy(0, n.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(HaveOccurred())
		Expect(backend.read()).To(HaveLen(1))
		Expect(loadRecoveryReceipts(n)[0].ResolvedAt).To(BeNil())
		grantRecoveryConnection(n, conn)
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(backend.read()).To(HaveLen(2))
	})
	ginkgo.It("keeps Kubernetes waitFor reevaluation at the exact original due time plus evaluation period", func() {
		n, config, payload := newRecoveryFixture()
		scraper := models.ConfigScraper{ID: uuid.New(), Name: uuid.NewString(), Spec: `{"kubernetes":[{}]}`, Source: models.SourceCRD}
		Expect(setup.DefaultContext.DB().Create(&scraper).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(setup.DefaultContext.DB().Model(&config).Update("scraper_id", nil).Error).To(Succeed())
			Expect(setup.DefaultContext.DB().Delete(&scraper).Error).To(Succeed())
		})
		Expect(setup.DefaultContext.DB().Model(&config).Update("scraper_id", scraper.ID).Error).To(Succeed())
		due := time.Now().Add(-time.Second).Truncate(time.Microsecond)
		history := models.NotificationSendHistory{ID: uuid.New(), NotificationID: n.ID, ResourceID: config.ID, SourceEvent: payload.EventName, Payload: payload.AsMap(), Status: models.NotificationStatusPending, NotBefore: &due}
		Expect(setup.DefaultContext.DB().Create(&history).Error).To(Succeed())
		handled, err := processRecoveryPending(setup.DefaultContext, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		var actual models.NotificationSendHistory
		Expect(setup.DefaultContext.DB().Where("id = ?", history.ID).First(&actual).Error).To(Succeed())
		Expect(actual.Status).To(Equal(models.NotificationStatusEvaluatingWaitFor))
		Expect(*actual.NotBefore).To(BeTemporally("==", due.Add(30*time.Second)))
		handled, err = processRecoveryPending(setup.DefaultContext, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeFalse())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&history).Update("not_before", time.Now().Add(-time.Second)).Error).To(Succeed())
		handled, err = processRecoveryPending(setup.DefaultContext, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})
	ginkgo.It("excludes concurrent claims, permits lease expiry takeover and rejects stale completion tokens", func() {
		n, _, payload := newRecoveryFixture()
		_, conn := newRecoverySMTP(n)
		ctx := recoveryContext(&n, payload)
		Expect(setRecoveryTransport(ctx, &conn, "")).To(Succeed())
		receipt, err := beginDelivery(ctx, "smtp", recoveryDestination{To: []string{"lease@example.com"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(receipt).Updates(map[string]any{"sent_at": time.Now(), "status": "sent"}).Error).To(Succeed())
		tokenA, tokenB := uuid.New(), uuid.New()
		a := setup.DefaultContext.DB().Begin()
		defer a.Rollback()
		var first []models.NotificationDelivery
		Expect(a.Raw(claimNotificationRecoverySQL, tokenA).Scan(&first).Error).To(Succeed())
		Expect(first).To(HaveLen(1))
		type result struct {
			rows []models.NotificationDelivery
			err  error
		}
		done := make(chan result, 1)
		go func() {
			var rows []models.NotificationDelivery
			err := setup.DefaultContext.DB().Raw(claimNotificationRecoverySQL, tokenB).Scan(&rows).Error
			done <- result{rows, err}
		}()
		var second result
		Eventually(done, "2s").Should(Receive(&second))
		Expect(second.err).NotTo(HaveOccurred())
		Expect(second.rows).To(BeEmpty())
		Expect(a.Commit().Error).To(Succeed())
		var rows []models.NotificationDelivery
		Expect(setup.DefaultContext.DB().Raw(claimNotificationRecoverySQL, tokenB).Scan(&rows).Error).To(Succeed())
		Expect(rows).To(BeEmpty())
		Expect(setup.DefaultContext.DB().Model(receipt).Update("lease_until", time.Now().Add(-time.Second)).Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Raw(claimNotificationRecoverySQL, tokenB).Scan(&rows).Error).To(Succeed())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].LeaseToken).To(Equal(&tokenB))
		stale := setup.DefaultContext.DB().Model(receipt).Where("lease_token = ? AND lease_until > NOW()", tokenA).Update("reply_at", time.Now())
		Expect(stale.Error).NotTo(HaveOccurred())
		Expect(stale.RowsAffected).To(BeZero())
	})
	ginkgo.It("uses shared system SMTP without named permission and preserves a person's original email after changes", func() {
		n, config, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		var system models.Connection
		Expect(setup.DefaultContext.DB().Where("name = ? AND type = ? AND deleted_at IS NULL", "system", models.ConnectionTypeEmail).First(&system).Error).To(Succeed())
		ginkgo.DeferCleanup(func() { Expect(setup.DefaultContext.DB().Save(&system).Error).To(Succeed()); mail.FlushSMTPCache() })
		Expect(setup.DefaultContext.DB().Model(&system).Updates(map[string]any{"url": conn.URL, "properties": conn.Properties}).Error).To(Succeed())
		mail.FlushSMTPCache()
		_, err := rbac.Enforcer().RemoveFilteredPolicy(0, n.ID.String())
		Expect(err).NotTo(HaveOccurred())
		person := models.Person{ID: uuid.New(), Name: "Recovery recipient", Email: "original-person@example.com"}
		Expect(setup.DefaultContext.DB().Create(&person).Error).To(Succeed())
		ginkgo.DeferCleanup(func() { Expect(setup.DefaultContext.DB().Delete(&person).Error).To(Succeed()) })
		payload.PersonID = &person.ID
		ctx := recoveryContext(&n, payload)
		backend.mu.Lock()
		backend.fail = true
		backend.mu.Unlock()
		Expect(sendPendingNotification(setup.DefaultContext, *ctx.log, payload)).To(HaveOccurred())
		before := loadRecoveryReceipts(n)[0]
		Expect(before.SentAt).To(BeNil())
		Expect(setup.DefaultContext.DB().Model(&person).Update("email", "").Error).To(Succeed())
		backend.mu.Lock()
		backend.fail = false
		backend.mu.Unlock()
		Expect(sendPendingNotification(setup.DefaultContext, *ctx.log, payload)).To(Succeed())
		Expect(loadRecoveryReceipts(n)[0].MessageID).To(Equal(before.MessageID))
		fresh := recoveryContext(&n, payload)
		Expect(sendPendingNotification(setup.DefaultContext, *fresh.log, payload)).To(HaveOccurred())
		Expect(loadRecoveryReceipts(n)).To(HaveLen(1))
		Expect(backend.read()).To(HaveLen(1))
		Expect(backend.read()[0].To).To(Equal([]string{"original-person@example.com"}))
		Expect(loadRecoveryReceipts(n)[0].ConnectionID).To(BeNil())
		Expect(setup.DefaultContext.DB().Model(&person).Update("email", "changed-person@example.com").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&n.Notification).Update("filter", "false").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(backend.read()).To(HaveLen(2))
		Expect(backend.read()[1].To).To(Equal(backend.read()[0].To))
		Expect(loadRecoveryReceipts(n)[0].ResolvedAt).NotTo(BeNil())
	})
	ginkgo.It("does not relabel a delayed warning source event as a later unhealthy episode", func() {
		n, config, _ := newRecoveryFixture()
		n.Events = []string{"config.warning"}
		n.CustomNotifications = []api.NotificationConfig{{URL: api.SystemSMTP + "?To=unused@example.com"}}
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "warning").Error).To(Succeed())
		var warning models.Event
		Expect(setup.DefaultContext.DB().Where("name = 'config.warning' AND event_id = ?", config.ID).First(&warning).Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "unhealthy").Error).To(Succeed())
		env, err := GetEnvForEvent(setup.DefaultContext, warning)
		Expect(err).NotTo(HaveOccurred())
		payloads, err := CreateNotificationSendPayloads(setup.DefaultContext, warning, &n, env)
		Expect(err).NotTo(HaveOccurred())
		Expect(payloads).To(BeEmpty())
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})

})
