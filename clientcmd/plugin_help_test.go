package clientcmd

import (
	"path/filepath"

	"github.com/flanksource/clicky/rpc"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("cached plugin command registration", func() {
	ginkgo.It("adds cached plugins under plugin and root help", func() {
		cleanup := manifestcache.SetDirForTest(filepath.Join(ginkgo.GinkgoT().TempDir(), "plugins"))
		defer cleanup()

		Expect(manifestcache.Write(manifestcache.Entry{
			Source:    manifestcache.SourceRemoteServer,
			ServerURL: "http://localhost:8080",
			Service: rpc.RPCService{
				Name:        "kubernetes-logs",
				Description: "Kubernetes logs",
				Version:     "v1.2.3",
				Operations: []rpc.RPCOperation{{
					Name:        "tail",
					Description: "Tail pod logs",
				}},
			},
		})).To(Succeed())

		root := &cobra.Command{Use: "incident-commander"}
		plugin := &cobra.Command{Use: "plugin <name> <operation>"}
		root.AddCommand(plugin)

		Expect(registerCachedPluginCommands(plugin, root)).To(Succeed())
		Expect(commandExists(plugin, "kubernetes-logs")).To(BeTrue())
		Expect(commandExists(root, "kubernetes-logs")).To(BeTrue())

		stdout, stderr, err := executeCommand(root, "plugin", "--help")
		Expect(err).NotTo(HaveOccurred())
		Expect(stderr).To(BeEmpty())
		Expect(stdout).To(ContainSubstring("kubernetes-logs"))

		stdout, stderr, err = executeCommand(root, "kubernetes-logs", "--help")
		Expect(err).NotTo(HaveOccurred())
		Expect(stderr).To(BeEmpty())
		Expect(stdout).To(ContainSubstring("Tail pod logs"))
		Expect(stdout).To(ContainSubstring("tail"))
	})

	ginkgo.It("does not replace existing static commands with plugin commands", func() {
		cleanup := manifestcache.SetDirForTest(filepath.Join(ginkgo.GinkgoT().TempDir(), "plugins"))
		defer cleanup()

		Expect(manifestcache.Write(manifestcache.Entry{
			Source: manifestcache.SourceRemoteServer,
			Service: rpc.RPCService{
				Name: "plugin",
				Operations: []rpc.RPCOperation{{
					Name: "run",
				}},
			},
		})).To(Succeed())

		root := &cobra.Command{Use: "incident-commander"}
		plugin := &cobra.Command{Use: "plugin <name> <operation>"}
		root.AddCommand(plugin)

		Expect(registerCachedPluginCommands(plugin, root)).To(Succeed())
		Expect(root.Commands()).To(HaveLen(1))
		Expect(commandExists(plugin, "plugin")).To(BeTrue())
	})
})
