package clientcmd

import (
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/rpc"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

// runnable returns a real (runnable) command so cobra lists it as an available
// command rather than an "additional help topic".
func runnable(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Run: func(*cobra.Command, []string) {}}
}

var _ = ginkgo.Describe("normalizeShort", func() {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "trims surrounding whitespace", in: "  hello  ", want: "hello"},
		{name: "strips trailing newline", in: "Make a request.\n", want: "Make a request."},
		{name: "keeps internal whitespace unchanged", in: "Deletes all failed  / completed", want: "Deletes all failed  / completed"},
	}
	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			Expect(normalizeShort(tt.in)).To(Equal(tt.want))
		})
	}
})

var _ = ginkgo.Describe("command grouping", func() {
	ginkgo.It("groups cached plugins under the Plugin Commands section", func() {
		cleanup := manifestcache.SetDirForTest(filepath.Join(ginkgo.GinkgoT().TempDir(), "plugins"))
		defer cleanup()

		Expect(manifestcache.Write(manifestcache.Entry{
			Source: manifestcache.SourceRemoteServer,
			Service: rpc.RPCService{
				Name:        "postgres",
				Description: "Postgres diagnostics",
				Operations:  []rpc.RPCOperation{{Name: "sessions"}},
			},
		})).To(Succeed())

		root := &cobra.Command{Use: "faro"}
		plugin := &cobra.Command{Use: "plugin"}
		root.AddCommand(plugin)
		root.AddCommand(runnable("version", "print version"))

		Expect(registerCachedPluginCommands(plugin, root)).To(Succeed())
		FinalizeCommandGroups(root)
		Expect(root.ContainsGroup(GroupPlugins)).To(BeTrue())
		Expect(root.ContainsGroup(GroupCore)).To(BeTrue())

		top := findChildCommand(root, "postgres")
		Expect(top).NotTo(BeNil())
		Expect(top.GroupID).To(Equal(GroupPlugins))

		// static command folded into Core
		Expect(findChildCommand(root, "version").GroupID).To(Equal(GroupCore))

		stdout, _, err := executeCommand(root, "--help")
		Expect(err).NotTo(HaveOccurred())
		Expect(stdout).To(ContainSubstring("Core Commands:"))
		Expect(stdout).To(ContainSubstring("Plugin Commands:"))
		Expect(stdout).To(ContainSubstring("postgres"))
		Expect(stdout).NotTo(ContainSubstring("Additional Commands:"))
	})

	ginkgo.It("groups cached playbooks under the Playbook Commands section", func() {
		root := &cobra.Command{Use: "faro"}
		playbook := &cobra.Command{Use: "playbook"}
		runCmd := &cobra.Command{Use: "run"}
		playbook.AddCommand(runCmd)
		root.AddCommand(playbook)

		Expect(registerCachedPlaybookCommands(playbook, root, []api.PlaybookListItem{{
			Name: "http",
		}})).To(Succeed())
		FinalizeCommandGroups(root)
		Expect(root.ContainsGroup(GroupPlaybooks)).To(BeTrue())

		top := findChildCommand(root, "http")
		Expect(top).NotTo(BeNil())
		Expect(top.GroupID).To(Equal(GroupPlaybooks))

		// the nested copy under `playbook run` stays ungrouped
		Expect(findChildCommand(runCmd, "http").GroupID).To(BeEmpty())

		stdout, _, err := executeCommand(root, "--help")
		Expect(err).NotTo(HaveOccurred())
		Expect(stdout).To(ContainSubstring("Playbook Commands:"))
	})

	ginkgo.It("keeps flat help output when there are no dynamic commands", func() {
		root := &cobra.Command{Use: "faro"}
		root.AddCommand(runnable("version", "print version"))
		root.AddCommand(runnable("whoami", "current context"))
		FinalizeCommandGroups(root)

		stdout, _, err := executeCommand(root, "--help")
		Expect(err).NotTo(HaveOccurred())
		// No dynamic commands => no grouping, original flat form.
		Expect(stdout).To(ContainSubstring("Available Commands:"))
		Expect(stdout).NotTo(ContainSubstring("Core Commands:"))
	})

	ginkgo.It("renders sections in Core, Plugin, Playbook order", func() {
		cleanup := manifestcache.SetDirForTest(filepath.Join(ginkgo.GinkgoT().TempDir(), "plugins"))
		defer cleanup()
		Expect(manifestcache.Write(manifestcache.Entry{
			Source:  manifestcache.SourceRemoteServer,
			Service: rpc.RPCService{Name: "s3", Operations: []rpc.RPCOperation{{Name: "ls"}}},
		})).To(Succeed())

		root := &cobra.Command{Use: "faro"}
		root.AddCommand(runnable("version", "v"))
		plugin := &cobra.Command{Use: "plugin"}
		root.AddCommand(plugin)
		playbook := &cobra.Command{Use: "playbook"}
		playbook.AddCommand(&cobra.Command{Use: "run"})
		root.AddCommand(playbook)

		// plugins are registered before playbooks (see SetupContextCachedPluginCommands)
		Expect(registerCachedPluginCommands(plugin, root)).To(Succeed())
		Expect(registerCachedPlaybookCommands(playbook, root, []api.PlaybookListItem{{Name: "http"}})).To(Succeed())
		FinalizeCommandGroups(root)

		stdout, _, err := executeCommand(root, "--help")
		Expect(err).NotTo(HaveOccurred())
		coreIdx := strings.Index(stdout, "Core Commands:")
		pluginIdx := strings.Index(stdout, "Plugin Commands:")
		playbookIdx := strings.Index(stdout, "Playbook Commands:")
		Expect(coreIdx).To(BeNumerically(">", -1))
		Expect(pluginIdx).To(BeNumerically(">", coreIdx))
		Expect(playbookIdx).To(BeNumerically(">", pluginIdx))
	})
})
