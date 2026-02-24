package db

import (
	"github.com/flanksource/duty/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("PersistPlaybookFromCRD", func() {
	It("records validation errors in status conditions", func() {
		playbook := &v1.Playbook{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "invalid-playbook",
				Namespace:  "default",
				Generation: 7,
			},
			Spec: v1.PlaybookSpec{
				Actions: []v1.PlaybookAction{
					{Name: "Wait for processing"},
				},
			},
		}

		err := PersistPlaybookFromCRD(DefaultContext, playbook)
		Expect(err).ToNot(HaveOccurred())
		Expect(playbook.Status.ObservedGeneration).To(Equal(int64(7)))

		condition := k8smeta.FindStatusCondition(playbook.Status.Conditions, v1.PlaybookConditionReady)
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(v1.PlaybookReasonValidationFailed))
		Expect(condition.Message).To(Equal(`action "Wait for processing" is empty`))

		var count int64
		err = DefaultContext.DB().Model(&models.Playbook{}).
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", playbook.Name, playbook.Namespace).
			Count(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})
})
