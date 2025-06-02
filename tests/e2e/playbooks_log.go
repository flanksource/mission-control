package e2e

import (
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Playbooks", func() {
	ginkgo.It("should log", func() {
		Expect(1).To(Equal(1))
	})

	ginkgo.It("should fail", func() {
		Expect(1).To(Equal(2))
	})
})
