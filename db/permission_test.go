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

var _ = Describe("PersistPermissionFromCRD", func() {
	It("returns error on invalid uid", func() {
		perm := &v1.Permission{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "invalid-permission-uid-" + uuid.NewString(),
				Namespace:  "default",
				UID:        "not-a-uuid",
				Generation: 3,
			},
			Spec: v1.PermissionSpec{
				Actions: []string{"read"},
				Subject: v1.PermissionSubject{Person: "admin@local"},
			},
		}

		err := PersistPermissionFromCRD(DefaultContext, perm)
		Expect(err).To(HaveOccurred())

		var count int64
		err = DefaultContext.DB().Model(&models.Permission{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", perm.Name, perm.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})

	It("persists permission on success", func() {
		uid := uuid.New()
		perm := &v1.Permission{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "valid-permission-" + uid.String(),
				Namespace:  "default",
				UID:        k8stypes.UID(uid.String()),
				Generation: 5,
			},
			Spec: v1.PermissionSpec{
				Actions: []string{"read"},
				Subject: v1.PermissionSubject{Person: "admin@local"},
				Object:  v1.PermissionObject{},
			},
		}

		err := PersistPermissionFromCRD(DefaultContext, perm)
		Expect(err).ToNot(HaveOccurred())

		var count int64
		err = DefaultContext.DB().Model(&models.Permission{}).
			Where("id = ? AND deleted_at IS NULL", uid).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(1)))
	})
})
