//go:build faro

package main

import (
	"github.com/flanksource/incident-commander/clientcmd"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("faro connections", func() {
	ginkgo.It("registers remote operations without local browser or CRD commands", func() {
		root := &cobra.Command{Use: "faro"}
		clientcmd.RegisterClientCommands(root)

		connection, _, err := root.Find([]string{"connection"})
		Expect(err).ToNot(HaveOccurred())
		Expect(connection).ToNot(BeNil())

		add, _, err := root.Find([]string{"connection", "add", "http"})
		Expect(err).ToNot(HaveOccurred())
		Expect(add).ToNot(BeNil())
		Expect(add.Flags().Lookup("dry-run")).To(BeNil())

		test, _, err := root.Find([]string{"connection", "test"})
		Expect(err).ToNot(HaveOccurred())
		Expect(test).ToNot(BeNil())

		for _, child := range connection.Commands() {
			Expect(child.Name()).ToNot(Equal("login"))
		}
	})
})
