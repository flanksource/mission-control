package actions

import (
	gocontext "context"
	"fmt"
	"io"
	"os"

	commons "github.com/flanksource/commons/context"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/pkg/clients/git"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
	"github.com/flanksource/gomplate/v3"
	gitv5 "github.com/go-git/go-git/v5"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var _ = ginkgo.Describe("Playbook Action Gitops", ginkgo.Label("slow"), ginkgo.Ordered, func() {
	var (
		spec v1.GitOpsAction
		env  TemplateEnv
	)

	ginkgo.It("should create a new git repository", func() {
		err := gitServer.InitRepo("testdata/dummy-repo", "main", "dummy-repo")
		Expect(err).To(BeNil())
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
				{
					Path: "notification.yaml",
					YQ:   `.metadata.name = "should-not-apply"`,
					If:   `request.parameter != ""`,
				},
			},
		}

		ctx := context.Context{
			Context: commons.NewContext(gocontext.TODO()),
		}
		ctx.Context.Logger.SetLogLevel("trace2")

		env = TemplateEnv{
			Params: map[string]any{
				"namespace": "logging",
			},
			Request: types.JSONMap{
				"parameter": "",
			},
		}

		templater := ctx.NewStructTemplater(env.AsMap(ctx), "template", nil)
		err := templater.Walk(&spec)
		Expect(err).To(BeNil())

		for i := range spec.Patches {
			if spec.Patches[i].If == "" {
				continue
			}
			val, err := gomplate.RunTemplate(env.AsMap(ctx), gomplate.Template{Expression: spec.Patches[i].If})
			Expect(err).To(BeNil())
			spec.Patches[i].If = val
		}

		var runner = GitOps{Context: ctx}
		res, err := runner.Run(ctx, spec)
		Expect(err).To(BeNil())
		Expect(len(res.Links)).To(BeZero())

		logger.Infof("Runner response: %#v", res)
	})

	ginkgo.It("should verify that the remote server has the proper changes", func() {
		var workTree *gitv5.Worktree
		var err error

		// should do a fresh clone
		{
			logger.Infof("Fresh cloning")
			_, workTree, err = git.Clone(context.New(), &connectors.GitopsAPISpec{
				Repository: gitServer.HTTPAddress() + "/dummy-repo",
				Base:       fmt.Sprintf("playbook-%s", env.Params["namespace"]),
				Branch:     fmt.Sprintf("playbook-%s", env.Params["namespace"]),
			})
			Expect(err).To(BeNil())
			logger.Infof("Cloned fresh repo to %s", workTree.Filesystem.Root())

			entries, err := os.ReadDir(workTree.Filesystem.Root())
			Expect(err).To(BeNil())
			for _, e := range entries {
				logger.Infof("Entry: %s", e.Name())
			}
		}

		// ensure the the patch was applied
		{
			txtFile, err := workTree.Filesystem.Open("notification.yaml")
			Expect(err).To(BeNil())

			content, err := io.ReadAll(txtFile)
			Expect(err).To(BeNil())

			var yamlContent map[string]any
			err = yaml.Unmarshal(content, &yamlContent)
			Expect(err).To(BeNil())

			metadata, ok := yamlContent["metadata"].(map[string]any)
			Expect(ok).To(BeTrue())

			Expect(metadata["namespace"].(string)).To(Equal(env.Params["namespace"]), "should have applied the patch")
			Expect(metadata["name"].(string)).To(Equal("http-check-passed"), "should skip patch when request.parameter is empty")
		}

		// ensure the new file was created
		{
			txtFile, err := workTree.Filesystem.Open(fmt.Sprintf("%s.txt", env.Params["namespace"]))
			Expect(err).To(BeNil())

			txtContent, err := io.ReadAll(txtFile)
			Expect(err).To(BeNil())

			Expect(string(txtContent)).To(Equal(spec.Files[0].Content), "should have created the new file")
		}
	})
})
