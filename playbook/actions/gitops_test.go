package actions

import (
	gocontext "context"
	"fmt"
	"io"
	"os"

	commons "github.com/flanksource/commons/context"
	"github.com/flanksource/commons/logger"
	gitv5 "github.com/go-git/go-git/v5"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/pkg/clients/git"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Playbook runner", ginkgo.Ordered, func() {
	var (
		spec v1.GitOpsAction
		env  TemplateEnv
	)

	ginkgo.It("should create a new git repository", func() {
		err := gitServer.InitRepo("testdata/dummy-repo", "main", "dummy-repo")
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("should run the GitOps action", func() {
		spec = v1.GitOpsAction{
			Repo: v1.GitOpsActionRepo{
				URL:    gitServer.HTTPAddress() + "/dummy-repo",
				Base:   "main",
				Branch: "playbook-{{.params.namespace}}",
			},
			Commit: v1.GitOpsActionCommit{
				AuthorName:  "Flank",
				AuthorEmail: "1bYpP@example.com",
				Message:     "Initial commit",
			},
			Files: []v1.GitOpsActionFile{
				{
					Path:    "{{.params.namespace}}.txt",
					Content: "dummy",
				},
			},
			Patches: []v1.GitOpsActionPatch{
				{
					Path: "notification.yaml",
					YQ:   `.metadata.namespace = "{{.params.namespace}}"`,
				},
			},
		}

		env = TemplateEnv{
			Params: map[string]string{
				"namespace": "logging",
			},
		}

		var runner GitOps
		ctx := context.Context{
			Context: commons.Context{
				Context: gocontext.Background(),
			},
		}
		res, err := runner.Run(ctx, spec, env)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.CreatedPR).To(Equal(0))
	})

	ginkgo.It("should verify that the remote server has the proper changes", func() {
		var workTree *gitv5.Worktree
		var err error

		// should do a fresh clone
		{
			_, workTree, err = git.Clone(gocontext.TODO(), &connectors.GitopsAPISpec{
				Repository: gitServer.HTTPAddress() + "/dummy-repo",
				Base:       fmt.Sprintf("playbook-%s", env.Params["namespace"]),
			})
			Expect(err).NotTo(HaveOccurred(), "could not clone from remote")

			entries, err := os.ReadDir(workTree.Filesystem.Root())
			Expect(err).NotTo(HaveOccurred())
			for _, e := range entries {
				logger.Infof("Entry: %s", e)
			}
		}

		// ensure the new file was created
		{
			txtFile, err := workTree.Filesystem.Open(fmt.Sprintf("%s.txt", env.Params["namespace"]))
			Expect(err).NotTo(HaveOccurred())

			txtContent, err := io.ReadAll(txtFile)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(txtContent)).To(Equal(spec.Files[0].Content))
		}

		// ensure the the patch was applied
		{
			txtFile, err := workTree.Filesystem.Open("notification.yaml")
			Expect(err).NotTo(HaveOccurred())

			content, err := io.ReadAll(txtFile)
			Expect(err).NotTo(HaveOccurred())

			var yamlContent map[string]any
			err = yaml.Unmarshal(content, &yamlContent)
			Expect(err).NotTo(HaveOccurred())

			metadata, ok := yamlContent["metadata"].(map[string]any)
			Expect(ok).To(BeTrue())

			Expect(metadata["namespace"].(string)).To(Equal(env.Params["namespace"]))
		}
	})
})
