package db

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("PersistConnectionFromCRD", func() {
	It("records uid parse errors in status", func() {
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
		Expect(err).ToNot(HaveOccurred())
		Expect(conn.Status.Ref).To(Equal(fmt.Sprintf("connection://%s/%s", conn.Namespace, conn.Name)))
		Expect(conn.Status.ObservedGeneration).To(Equal(int64(3)))

		condition := k8smeta.FindStatusCondition(conn.Status.Conditions, v1.ConditionReady)
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(v1.ReadyReasonPersistFailed))
		Expect(condition.Message).To(ContainSubstring("failed to parse uid"))

		var count int64
		err = DefaultContext.DB().Model(&models.Connection{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", conn.Name, conn.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})

	It("records ready condition and preserves status ref on success", func() {
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
		Expect(conn.Status.ObservedGeneration).To(Equal(int64(9)))

		condition := k8smeta.FindStatusCondition(conn.Status.Conditions, v1.ConditionReady)
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(v1.ReadyReasonSynced))
		Expect(condition.Message).To(Equal("Connection is valid and persisted"))

		var count int64
		err = DefaultContext.DB().Model(&models.Connection{}).
			Where("id = ? AND deleted_at IS NULL", uid).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(1)))
	})
})
