//go:build recoverytests

package notification

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = ginkgo.Describe("Notification recovery retries", func() {
	for _, tc := range []struct {
		name, property string
		retries        int
	}{
		{name: "defaults to seven retries", retries: 7},
		{name: "uses a configured retry limit", property: "2", retries: 2},
		{name: "allows zero retries", property: "0", retries: 0},
		{name: "treats negative limits as zero", property: "-1", retries: 0},
		{name: "falls back to seven for invalid limits", property: "invalid", retries: 7},
		{name: "caps exponential backoff at one hour", property: "12", retries: 12},
	} {
		ginkgo.It(tc.name, func() {
			n, config, payload := newRecoveryFixture()
			backend, conn := newRecoverySMTP(n)
			ctx := setup.DefaultContext
			if tc.property != "" {
				ctx = ctx.WithObject(metav1.ObjectMeta{Name: uuid.NewString(), Annotations: map[string]string{
					"mission-control/notification.recovery.max-retries": tc.property,
				}})
			}
			_, err := SendRawNotification(recoveryContext(&n, payload), conn.ID.String(), "", nil, NotificationTemplate{Message: "unhealthy"}, &n)
			Expect(err).NotTo(HaveOccurred())
			_, err = rbac.Enforcer().RemoveFilteredPolicy(0, n.ID.String())
			Expect(err).NotTo(HaveOccurred())

			for range 3 {
				readyRecoveries(n)
				Expect(ReconcileNotificationRecoveries(ctx)).To(Succeed())
				receipt := loadRecoveryReceipts(n)[0]
				Expect(receipt.Status).To(Equal("waiting-for-healthy"))
				Expect(receipt.Attempts).To(BeZero())
			}
			Expect(ctx.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
			for attempt := 1; attempt <= tc.retries+1; attempt++ {
				readyRecoveries(n)
				before := time.Now()
				Expect(ReconcileNotificationRecoveries(ctx)).To(HaveOccurred())
				after := time.Now()
				receipt := loadRecoveryReceipts(n)[0]
				Expect(receipt.Attempts).To(Equal(attempt))
				Expect(receipt.ResolvedAt).To(BeNil())
				Expect(receipt.LeaseToken).To(BeNil())
				Expect(receipt.LeaseUntil).To(BeNil())
				if attempt <= tc.retries {
					Expect(receipt.Status).To(Equal("recovery-error"))
					delay := min(time.Hour, time.Duration(1<<min(attempt, 12))*time.Second)
					Expect(receipt.NotBefore).To(BeTemporally(">=", before.Add(delay).Add(-time.Millisecond)))
					Expect(receipt.NotBefore).To(BeTemporally("<=", after.Add(delay).Add(time.Millisecond)))
				} else {
					Expect(receipt.Status).To(Equal("recovery-exhausted"))
					Expect(receipt.Error).NotTo(BeNil())
					Expect(*receipt.Error).To(ContainSubstring("retry limit exhausted"))
				}
			}
			grantRecoveryConnection(n, conn)
			readyRecoveries(n)
			Expect(ReconcileNotificationRecoveries(ctx)).To(Succeed())
			Expect(loadRecoveryReceipts(n)[0].Attempts).To(Equal(tc.retries + 1))
			Expect(loadRecoveryReceipts(n)[0].Status).To(Equal("recovery-exhausted"))
			Expect(backend.read()).To(HaveLen(1))
		})
	}

	ginkgo.It("resolves successfully on the final allowed retry", func() {
		n, config, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		_, err := SendRawNotification(recoveryContext(&n, payload), conn.ID.String(), "", nil, NotificationTemplate{Message: "unhealthy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&models.NotificationDelivery{}).Where("notification_id = ?", n.ID).Update("attempts", 7).Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		receipt := loadRecoveryReceipts(n)[0]
		Expect(receipt.Attempts).To(Equal(8))
		Expect(receipt.Status).To(Equal("resolved"))
		Expect(receipt.ResolvedAt).NotTo(BeNil())
		Expect(backend.read()).To(HaveLen(2))
	})

	ginkgo.It("does not send for existing receipts already beyond the configured limit", func() {
		n, config, payload := newRecoveryFixture()
		backend, conn := newRecoverySMTP(n)
		_, err := SendRawNotification(recoveryContext(&n, payload), conn.ID.String(), "", nil, NotificationTemplate{Message: "unhealthy"}, &n)
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&models.NotificationDelivery{}).Where("notification_id = ?", n.ID).Update("attempts", 8).Error).To(Succeed())
		readyRecoveries(n)
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(HaveOccurred())
		Expect(loadRecoveryReceipts(n)[0].Status).To(Equal("recovery-exhausted"))
		Expect(loadRecoveryReceipts(n)[0].ResolvedAt).To(BeNil())
		Expect(backend.read()).To(HaveLen(1))
	})
})
