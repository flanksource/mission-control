package clientcmd

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("retry flags", func() {
	// The flag names and defaults are the whole user-facing contract of the retry policy, and
	// nothing else in the suite would notice a rename or a changed default.
	ginkgo.It("offers --retries and --retry-delay on every client command", func() {
		root := &cobra.Command{Use: "test"}

		RegisterClientCommands(root)

		Expect(root.PersistentFlags().Lookup("retries").DefValue).To(Equal("3"))
		Expect(root.PersistentFlags().Lookup("retry-delay").DefValue).To(Equal("1s"))
	})

	ginkgo.It("carries parsed values through to the client policy", func() {
		root := &cobra.Command{Use: "test"}
		RegisterClientCommands(root)
		ginkgo.DeferCleanup(func(retries int, delay time.Duration) {
			retriesFlag, retryDelayFlag = retries, delay
		}, retriesFlag, retryDelayFlag)

		Expect(root.PersistentFlags().Parse([]string{"--retries=7", "--retry-delay=250ms"})).To(Succeed())

		Expect(retriesFlag).To(Equal(7))
		Expect(retryDelayFlag).To(Equal(250 * time.Millisecond))
	})
})
