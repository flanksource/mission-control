package db

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("Notification Silence", ginkgo.Ordered, func() {
	var silences []models.NotificationSilence
	ginkgo.BeforeAll(func() {
		silences = []models.NotificationSilence{
			{
				ID:     uuid.New(),
				From:   lo.ToPtr(time.Now().Add(-time.Hour)),
				Until:  lo.ToPtr(time.Now().Add(time.Hour)),
				Source: models.SourceCRD,
				NotificationSilenceResource: models.NotificationSilenceResource{
					ConfigID: lo.ToPtr(dummy.EKSCluster.ID.String()),
				},
			},
			{
				ID:        uuid.New(),
				From:      lo.ToPtr(time.Now().Add(-time.Hour)),
				Until:     lo.ToPtr(time.Now().Add(time.Hour)),
				Source:    models.SourceCRD,
				Recursive: true,
				NotificationSilenceResource: models.NotificationSilenceResource{
					ConfigID: lo.ToPtr(dummy.LogisticsAPIDeployment.ID.String()),
				},
			},
			{
				ID:        uuid.New(),
				From:      lo.ToPtr(time.Now().Add(-time.Hour)),
				Until:     lo.ToPtr(time.Now().Add(time.Hour)),
				Source:    models.SourceCRD,
				Recursive: true,
				NotificationSilenceResource: models.NotificationSilenceResource{
					ComponentID: lo.ToPtr(dummy.Logistics.ID.String()),
				},
			},
		}

		err := DefaultContext.DB().Create(&silences).Error
		Expect(err).To(BeNil())
	})

	ginkgo.Context("non recursive match", func() {
		ginkgo.It("should match", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ConfigID: lo.ToPtr(dummy.EKSCluster.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(1))
		})

		ginkgo.It("should not match", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ConfigID: lo.ToPtr(dummy.KubernetesCluster.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(0))
		})
	})

	ginkgo.Context("config recursive match", func() {
		ginkgo.It("should match a child", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ConfigID: lo.ToPtr(dummy.LogisticsAPIReplicaSet.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(1))
		})

		ginkgo.It("should match a grand child", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ConfigID: lo.ToPtr(dummy.LogisticsAPIPodConfig.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(1))
		})

		ginkgo.It("should not match", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ConfigID: lo.ToPtr(dummy.LogisticsUIDeployment.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(0))
		})
	})

	ginkgo.Context("component recursive match", func() {
		ginkgo.It("should match a child", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ComponentID: lo.ToPtr(dummy.LogisticsAPI.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(1))
		})

		ginkgo.It("should match a grand child", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ComponentID: lo.ToPtr(dummy.LogisticsWorker.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(1))
		})

		ginkgo.It("should not match", func() {
			matched, err := GetMatchingNotificationSilences(DefaultContext, models.NotificationSilenceResource{ComponentID: lo.ToPtr(dummy.ClusterComponent.ID.String())})
			Expect(err).To(BeNil())
			Expect(len(matched)).To(Equal(0))
		})
	})
})
