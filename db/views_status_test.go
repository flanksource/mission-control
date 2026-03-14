package db

import (
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("PersistViewFromCRD", func() {
	It("returns validation errors", func() {
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
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("view must have at least one query"))

		var count int64
		err = DefaultContext.DB().Model(&models.View{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", view.Name, view.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})

	It("returns error on invalid uid", func() {
		view := &v1.View{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "invalid-view-uid",
				Namespace:  "default",
				UID:        "not-a-uuid",
				Generation: 3,
			},
		}

		err := PersistViewFromCRD(DefaultContext, view)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse uid"))

		var count int64
		err = DefaultContext.DB().Model(&models.View{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", view.Name, view.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})
})
