package db

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("PersistConnectionFromCRD", func() {
	It("returns error on invalid uid", func() {
		conn := &v1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "invalid-connection-uid-" + uuid.NewString(),
				Namespace:  "default",
				UID:        "not-a-uuid",
				Generation: 3,
			},
			Spec: v1.ConnectionSpec{
				HTTP: &v1.ConnectionHTTP{URL: "http://example.com"},
			},
		}

		err := PersistConnectionFromCRD(DefaultContext, conn)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse uid"))

		var count int64
		err = DefaultContext.DB().Model(&models.Connection{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", conn.Name, conn.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})

	It("persists connection and preserves status ref on success", func() {
		uid := uuid.New()
		conn := &v1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "valid-connection-" + uid.String(),
				Namespace:  "default",
				UID:        k8stypes.UID(uid.String()),
				Generation: 9,
			},
			Spec: v1.ConnectionSpec{
				HTTP: &v1.ConnectionHTTP{URL: "http://example.com"},
			},
		}

		err := PersistConnectionFromCRD(DefaultContext, conn)
		Expect(err).ToNot(HaveOccurred())
		Expect(conn.Status.Ref).To(Equal(fmt.Sprintf("connection://%s/%s", conn.Namespace, conn.Name)))

		var count int64
		err = DefaultContext.DB().Model(&models.Connection{}).
			Where("id = ? AND deleted_at IS NULL", uid).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(1)))
	})
})
