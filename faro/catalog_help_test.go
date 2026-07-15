package main

import (
	"bytes"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("faro catalog help", func() {
	ginkgo.It("keeps the catalog root help-only instead of defaulting to list", func() {
		catalogCmd := &cobra.Command{
			Use: "catalog",
			RunE: func(cmd *cobra.Command, args []string) error {
				return nil
			},
		}
		catalogCmd.Flags().String("type", "", "promoted list flag")
		catalogCmd.AddCommand(&cobra.Command{Use: "list"})

		documentCatalogCommand(catalogCmd)

		Expect(catalogCmd.Runnable()).To(BeFalse())
		Expect(catalogCmd.Flags().Lookup("type")).To(BeNil())

		var out bytes.Buffer
		catalogCmd.SetOut(&out)
		catalogCmd.SetErr(&out)
		catalogCmd.SetArgs([]string{})
		Expect(catalogCmd.Execute()).To(Succeed())
		Expect(out.String()).To(ContainSubstring("Choose a subcommand"))
		Expect(out.String()).To(ContainSubstring("faro catalog list"))
	})
})
