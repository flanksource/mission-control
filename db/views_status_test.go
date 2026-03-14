package db

import (
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("PersistViewFromCRD", func() {
	It("records validation errors in status", func() {
		view := &v1.View{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "invalid-view",
				Namespace:  "default",
				UID:        k8stypes.UID(uuid.NewString()),
				Generation: 7,
			},
			Spec: v1.ViewSpec{},
		}

		err := PersistViewFromCRD(DefaultContext, view)
		Expect(err).ToNot(HaveOccurred())
		Expect(view.Status.ObservedGeneration).To(Equal(int64(7)))

		condition := k8smeta.FindStatusCondition(view.Status.Conditions, v1.ConditionReady)
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(v1.ReadyReasonValidationFailed))
		Expect(condition.Message).To(Equal("view must have at least one query"))

		var count int64
		err = DefaultContext.DB().Model(&models.View{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", view.Name, view.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})

	It("records uid parse errors in status", func() {
		view := &v1.View{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "invalid-view-uid",
				Namespace:  "default",
				UID:        "not-a-uuid",
				Generation: 3,
			},
		}

		err := PersistViewFromCRD(DefaultContext, view)
		Expect(err).ToNot(HaveOccurred())
		Expect(view.Status.ObservedGeneration).To(Equal(int64(3)))

		condition := k8smeta.FindStatusCondition(view.Status.Conditions, v1.ConditionReady)
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(v1.ReadyReasonPersistFailed))
		Expect(condition.Message).To(ContainSubstring("failed to parse uid"))

		var count int64
		err = DefaultContext.DB().Model(&models.View{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", view.Name, view.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})
})
