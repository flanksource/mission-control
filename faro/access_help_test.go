package main

import (
	"bytes"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("documentAccessCommand", func() {
	newAccessCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "access", RunE: func(*cobra.Command, []string) error { return nil }}
		cmd.Flags().String("type", "", "list filter promoted onto the root")
		for _, name := range []string{"users", "groups", "roles", "permissions", "logs", "reviews"} {
			cmd.AddCommand(&cobra.Command{Use: name})
		}
		return cmd
	}

	ginkgo.It("turns the access root into a help-only command", func() {
		cmd := newAccessCmd()

		documentAccessCommand(cmd)

		Expect(cmd.Runnable()).To(BeFalse())
		Expect(cmd.Flags().Lookup("type")).To(BeNil())
	})

	ginkgo.It("renders the examples when invoked with no subcommand", func() {
		cmd := newAccessCmd()
		documentAccessCommand(cmd)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{})

		Expect(cmd.Execute()).To(Succeed())

		Expect(out.String()).To(ContainSubstring("faro access permissions"))
	})

	for _, name := range []string{"users", "groups", "roles"} {
		ginkgo.It(fmt.Sprintf("documents the %s subcommand", name), func() {
			cmd := newAccessCmd()

			documentAccessCommand(cmd)

			sub := subcommand(cmd, name)
			Expect(sub).ToNot(BeNil())
			Expect(sub.Short).ToNot(BeEmpty())
			Expect(sub.Example).To(ContainSubstring("faro access " + name))
		})
	}
})
