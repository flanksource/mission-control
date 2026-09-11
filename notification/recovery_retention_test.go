//go:build recoverytests

package notification

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Notification recovery retention", func() {
	for _, terminal := range []struct {
		status, column string
		age            time.Duration
	}{{"resolved", "resolved_at", 30 * 24 * time.Hour}, {"recovery-exhausted", "exhausted_at", 90 * 24 * time.Hour}} {
		ginkgo.It("uses only the explicit "+terminal.column+" boundary and protects ambiguous, active and leased receipts", func() {
			n, _, payload := newRecoveryFixture()
			now := time.Now().Truncate(time.Microsecond)
			var deleted []uuid.UUID
			for _, tc := range []struct {
				name           string
				age            *time.Time
				status         string
				unsent, leased bool
			}{
				{"old", timePointer(now.Add(-terminal.age - time.Microsecond)), terminal.status, false, false},
				{"boundary", timePointer(now.Add(-terminal.age)), terminal.status, false, false},
				{"recent", timePointer(now.Add(-terminal.age + time.Microsecond)), terminal.status, false, false},
				{"legacy-null", nil, terminal.status, false, false},
				{"waiting", timePointer(now.Add(-2 * terminal.age)), "waiting-for-healthy", false, false},
				{"dispatching", timePointer(now.Add(-2 * terminal.age)), "dispatching", true, false},
				{"prepared", timePointer(now.Add(-2 * terminal.age)), "prepared", true, false},
				{"leased", timePointer(now.Add(-2 * terminal.age)), terminal.status, false, true},
			} {
				receipt := pollingReceipt(n, payload)
				values := map[string]any{"status": tc.status, terminal.column: tc.age, "created_at": now.Add(-365 * 24 * time.Hour)}
				if tc.unsent {
					values["sent_at"] = nil
				}
				if tc.leased {
					values["lease_until"] = now.Add(time.Hour)
				}
				Expect(setup.DefaultContext.DB().Model(&receipt).Updates(values).Error).To(Succeed())
				if tc.name == "old" {
					deleted = append(deleted, receipt.ID)
				}
			}
			Expect(cleanupNotificationRecoveries(setup.DefaultContext, now)).To(Succeed())
			receipts := loadRecoveryReceipts(n)
			Expect(receipts).To(HaveLen(7))
			for _, receipt := range receipts {
				Expect(deleted).NotTo(ContainElement(receipt.ID))
			}
		})
	}

	for _, invalid := range []string{"0", "-1h", "invalid"} {
		ginkgo.It("safely disables all retention categories for "+invalid, func() {
			n, _, payload := newRecoveryFixture()
			receipt := pollingReceipt(n, payload)
			Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "resolved", "resolved_at": time.Now().Add(-365 * 24 * time.Hour)}).Error).To(Succeed())
			ctx := recoveryProperties(map[string]string{"retention.resolved": invalid, "retention.exhausted": invalid, "retention.episodes": invalid, "retention.deleted-states": invalid})
			for _, key := range []string{"resolved", "exhausted", "episodes", "deleted-states"} {
				Expect(recoveryRetentionDuration(ctx, key, time.Hour)).To(BeZero())
			}
			before := readPollingReceipt(receipt.ID)
			Expect(CleanupNotificationRecoveries(ctx)).To(Succeed())
			Expect(readPollingReceipt(receipt.ID)).To(Equal(before))
		})
	}

	for _, invalid := range []string{"0", "-1", "invalid"} {
		ginkgo.It("uses safe interval/batch/budget defaults for "+invalid, func() {
			ctx := recoveryProperties(map[string]string{"retention.interval": invalid, "retention.batch-size": invalid, "retention.budget": invalid, "sweep.interval": invalid, "sweep.batch-size": invalid, "sweep.budget": invalid, "wake.batch-size": invalid})
			Expect(CleanupNotificationRecoveriesJob(ctx).Schedule).To(Equal("@every 1h0m0s"))
			Expect(SweepNotificationRecoveriesJob(ctx).Schedule).To(Equal("@every 5m0s"))
			Expect(recoveryPositiveInt(ctx, "retention.batch-size", 500)).To(Equal(500))
			Expect(recoveryPositiveInt(ctx, "sweep.batch-size", 100)).To(Equal(100))
			Expect(recoveryPositiveInt(ctx, "wake.batch-size", 20)).To(Equal(20))
			Expect(recoveryPositiveDuration(ctx, "retention.budget", 5*time.Second)).To(Equal(5 * time.Second))
			Expect(recoveryPositiveDuration(ctx, "sweep.budget", 5*time.Second)).To(Equal(5 * time.Second))
		})
	}

	ginkgo.It("bounds batches and excludes concurrent processes using a transaction advisory lock", func() {
		n, _, payload := newRecoveryFixture()
		for range 3 {
			receipt := pollingReceipt(n, payload)
			Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "resolved", "resolved_at": time.Now().Add(-365 * 24 * time.Hour)}).Error).To(Succeed())
		}
		ctx := recoveryProperties(map[string]string{"retention.batch-size": "1"})
		tx := ctx.DB().Begin()
		defer tx.Rollback()
		Expect(tx.Exec("SELECT pg_advisory_xact_lock(?)", recoveryRetentionLock).Error).To(Succeed())
		done := make(chan error, 1)
		go func() { done <- CleanupNotificationRecoveries(ctx) }()
		Eventually(done, "2s").Should(Receive(BeNil()))
		Expect(loadRecoveryReceipts(n)).To(HaveLen(3))
		Expect(tx.Rollback().Error).To(Succeed())
		Expect(CleanupNotificationRecoveries(ctx)).To(Succeed())
		Expect(loadRecoveryReceipts(n)).To(HaveLen(2))
	})

	ginkgo.It("prunes only ended episodes unreferenced by ANY receipt or current state", func() {
		n, _, payload := newRecoveryFixture()
		tx := setup.DefaultContext.DB().Begin()
		defer tx.Rollback()
		ctx := setup.DefaultContext.WithDB(tx, nil)
		old := time.Now().Add(-60 * 24 * time.Hour)
		episodes := make([]models.NotificationHealthEpisode, 4)
		for i := range episodes {
			episodes[i] = models.NotificationHealthEpisode{ID: uuid.New(), ResourceType: "config", ResourceID: uuid.New(), StartedAt: old.Add(-time.Hour), HealthyAt: &old}
			if i == 3 {
				episodes[i].HealthyAt = nil
			}
			Expect(tx.Create(&episodes[i]).Error).To(Succeed())
		}
		receipt := pollingReceipt(n, payload)
		Expect(tx.Model(&receipt).Updates(map[string]any{"episode_id": episodes[1].ID, "sent_at": nil, "status": "dispatching"}).Error).To(Succeed())
		Expect(tx.Exec("INSERT INTO notification_health_states(resource_type,resource_id,generation,health,episode_id) VALUES ('config',?,?,'healthy',?)", episodes[2].ResourceID, uuid.New(), episodes[2].ID).Error).To(Succeed())
		Expect(CleanupNotificationRecoveries(ctx)).To(Succeed())
		var remaining []uuid.UUID
		Expect(tx.Model(&models.NotificationHealthEpisode{}).Where("id IN ?", []uuid.UUID{episodes[0].ID, episodes[1].ID, episodes[2].ID, episodes[3].ID}).Pluck("id", &remaining).Error).To(Succeed())
		Expect(remaining).To(ConsistOf(episodes[1].ID, episodes[2].ID, episodes[3].ID))
	})

	ginkgo.It("requires actual source absence, observation grace and no outstanding dependencies for deleted states", func() {
		_, live, _ := newRecoveryFixture()
		tx := setup.DefaultContext.DB().Begin()
		defer tx.Rollback()
		ctx := setup.DefaultContext.WithDB(tx, nil)
		now := time.Now().Truncate(time.Microsecond)
		old := now.Add(-31 * 24 * time.Hour)
		for _, tc := range []struct {
			name       string
			observed   *time.Time
			live, open bool
			health     string
		}{
			{"absent", &old, false, false, "deleted"},
			{"boundary", timePointer(now.Add(-30 * 24 * time.Hour)), false, false, "deleted"},
			{"legacy", nil, false, false, "deleted"},
			{"live", &old, true, false, "deleted"},
			{"open-outage", &old, false, true, "deleted"},
			{"healthy", &old, false, false, "healthy"},
		} {
			id := uuid.New()
			if tc.live {
				id = live.ID
			}
			Expect(tx.Exec(`INSERT INTO notification_health_states(resource_type,resource_id,generation,health,deletion_observed_at) VALUES ('config',?,?,?,?)
    ON CONFLICT (resource_type,resource_id) DO UPDATE SET health=excluded.health,deletion_observed_at=excluded.deletion_observed_at`, id, uuid.New(), tc.health, tc.observed).Error).To(Succeed())
			if tc.open {
				Expect(tx.Create(&models.NotificationHealthEpisode{ID: uuid.New(), ResourceType: "config", ResourceID: id, StartedAt: old}).Error).To(Succeed())
			}
			Expect(cleanupNotificationRecoveries(ctx, now)).To(Succeed())
			var count int64
			Expect(tx.Model(&models.NotificationHealthState{}).Where("resource_type='config' AND resource_id=?", id).Count(&count).Error).To(Succeed())
			if tc.name == "absent" {
				Expect(count).To(BeZero())
			} else {
				Expect(count).To(Equal(int64(1)), tc.name)
			}
		}
	})

	ginkgo.It("retains physically soft-deleted sources and any receipts referencing a deleted resource", func() {
		n, config, payload := newRecoveryFixture()
		receipt := pollingReceipt(n, payload)
		tx := setup.DefaultContext.DB().Begin()
		defer tx.Rollback()
		ctx := setup.DefaultContext.WithDB(tx, nil)
		old := time.Now().Add(-60 * 24 * time.Hour)
		Expect(tx.Model(&config).Update("deleted_at", old).Error).To(Succeed())
		Expect(tx.Exec("UPDATE notification_health_states SET deletion_observed_at=? WHERE resource_type='config' AND resource_id=?", old, config.ID).Error).To(Succeed())
		Expect(tx.Exec("UPDATE notification_health_episodes SET healthy_at=? WHERE id=?", old, receipt.EpisodeID).Error).To(Succeed())
		Expect(tx.Model(&receipt).Updates(map[string]any{"sent_at": nil, "status": "dispatching"}).Error).To(Succeed())
		for _, physicallyDelete := range []bool{false, true} {
			if physicallyDelete {
				Expect(tx.Delete(&config).Error).To(Succeed())
				Expect(tx.Exec("UPDATE notification_health_states SET deletion_observed_at=? WHERE resource_type='config' AND resource_id=?", old, config.ID).Error).To(Succeed())
			}
			Expect(CleanupNotificationRecoveries(ctx)).To(Succeed())
			var count int64
			Expect(tx.Model(&models.NotificationHealthState{}).Where("resource_type='config' AND resource_id=?", config.ID).Count(&count).Error).To(Succeed())
			Expect(count).To(Equal(int64(1)))
		}
	})

	ginkgo.It("skips a row locked by another process and cleans it on a later invocation", func() {
		n, _, payload := newRecoveryFixture()
		receipt := pollingReceipt(n, payload)
		Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "resolved", "resolved_at": time.Now().Add(-60 * 24 * time.Hour)}).Error).To(Succeed())
		tx := setup.DefaultContext.DB().Begin()
		defer tx.Rollback()
		Expect(tx.Exec("SELECT 1 FROM notification_deliveries WHERE id=? FOR UPDATE", receipt.ID).Error).To(Succeed())
		done := make(chan error, 1)
		go func() { done <- CleanupNotificationRecoveries(setup.DefaultContext) }()
		Eventually(done, "2s").Should(Receive(BeNil()))
		Expect(loadRecoveryReceipts(n)).To(HaveLen(1))
		Expect(tx.Rollback().Error).To(Succeed())
		Expect(CleanupNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})

	ginkgo.It("rolls back cleanup when its short runtime budget is exceeded", func() {
		n, _, payload := newRecoveryFixture()
		receipt := pollingReceipt(n, payload)
		Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "resolved", "resolved_at": time.Now().Add(-60 * 24 * time.Hour)}).Error).To(Succeed())
		tx := setup.DefaultContext.DB().Begin()
		defer tx.Rollback()
		Expect(tx.Exec("LOCK TABLE config_items IN ACCESS EXCLUSIVE MODE").Error).To(Succeed())
		ctx := recoveryProperties(map[string]string{"retention.budget": "20ms"})
		done := make(chan error, 1)
		go func() { done <- CleanupNotificationRecoveries(ctx) }()
		Eventually(done, "2s").Should(Receive(HaveOccurred()))
		Expect(loadRecoveryReceipts(n)).To(HaveLen(1))
		Expect(tx.Rollback().Error).To(Succeed())
		Expect(CleanupNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(loadRecoveryReceipts(n)).To(BeEmpty())
	})

	ginkgo.It("stamps a fresh exhaustion window after explicit operator reset", func() {
		n, config, payload := newRecoveryFixture()
		receipt := pollingReceipt(n, payload)
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "recovery-error", "attempts": 8, "exhausted_at": time.Now().Add(-100 * 24 * time.Hour)}).Error).To(Succeed())
		before := time.Now()
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(HaveOccurred())
		first := readPollingReceipt(receipt.ID)
		Expect(first.ExhaustedAt).NotTo(BeNil())
		Expect(*first.ExhaustedAt).To(BeTemporally(">=", before.Add(-time.Millisecond)))
		Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "recovery-error", "attempts": 0, "exhausted_at": nil, "not_before": time.Now().Add(-time.Second), "policy": `{"enabled":true}`}).Error).To(Succeed())
		ctx := recoveryProperties(map[string]string{"max-retries": "0"})
		Expect(ReconcileNotificationRecoveries(ctx)).To(HaveOccurred())
		second := readPollingReceipt(receipt.ID)
		Expect(second.ExhaustedAt).NotTo(BeNil())
		Expect(*second.ExhaustedAt).To(BeTemporally(">", *first.ExhaustedAt))
	})
})

func timePointer(t time.Time) *time.Time { return &t }
