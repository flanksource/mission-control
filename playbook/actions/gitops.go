package actions

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/pkg/clients/git"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
	gitv5 "github.com/go-git/go-git/v5"
)

type GitOps struct {
	workTree *gitv5.Worktree
	spec     *connectors.GitopsAPISpec
	env      TemplateEnv

	shouldCreatePR bool
}

type GitOpsActionResult struct {
	CreatedPR int `json:"createdPR,omitempty"`
}

func (t *GitOps) Run(ctx context.Context, action v1.GitOpsAction, env TemplateEnv) (*GitOpsActionResult, error) {
	var response GitOpsActionResult

	if len(action.Patches) == 0 && len(action.Files) == 0 {
		logger.Warnf("no patches or files specified on gitops action")
		return nil, nil
	}

	t.env = env

	if err := t.generateSpec(ctx, action); err != nil {
		return nil, err
	}

	connector, workTree, err := t.cloneRepo(ctx, action)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repo: %w", err)
	}
	t.workTree = workTree

	if err := t.applyPatches(ctx, action); err != nil {
		return nil, fmt.Errorf("failed to apply patches: %w", err)
	}

	if err := t.modifyFiles(ctx, action); err != nil {
		return nil, fmt.Errorf("failed to modify files: %w", err)
	}

	if err := git.CommitAndPush(ctx, connector, workTree, t.spec); err != nil {
		return nil, fmt.Errorf("failed to commit and push: %w", err)
	}

	if t.shouldCreatePR {
		prNumber, err := t.createPR(ctx, connector, workTree)
		if err != nil {
			return nil, err
		}
		response.CreatedPR = prNumber
	}

	return &response, nil
}

// generateSpec generates the spec for the git client from the action
func (t *GitOps) generateSpec(ctx context.Context, action v1.GitOpsAction) error {
	var err error

	if action.Repo.Base == "" {
		action.Repo.Base = "master"
	}

	if action.Repo.Branch == "" {
		action.Repo.Branch = action.Repo.Base
	} else {
		action.Repo.Branch, err = gomplate.RunTemplate(t.env.AsMap(), gomplate.Template{Template: action.Repo.Branch})
		if err != nil {
			return err
		}
	}

	t.spec = &connectors.GitopsAPISpec{
		Repository: action.Repo.URL,
		Base:       action.Repo.Base,
		Branch:     action.Repo.Branch,
		CommitMsg:  action.Commit.Message,
		User:       action.Commit.AuthorName,
		Email:      action.Commit.AuthorEmail,
	}

	if t.spec.CommitMsg != "" {
		t.spec.CommitMsg, err = gomplate.RunTemplate(t.env.AsMap(), gomplate.Template{Template: t.spec.CommitMsg})
		if err != nil {
			return err
		}
	}

	if action.Repo.Connection != "" {
		conn, err := ctx.HydrateConnectionByURL(action.Repo.Connection)
		if err != nil {
			return err
		}

		switch conn.Type {
		case models.ConnectionTypeGithub:
			t.spec.GITHUB_TOKEN = conn.Password
			t.shouldCreatePR = true

		case models.ConnectionTypeAzureDevops:
			// TODO: Azure devops connection doesn't have git credentials ...?

		case models.ConnectionTypeGit:
			// TODO: need to finalize this once git connection is implemented

		default:
			return fmt.Errorf("unsupported connection type: %s", conn.Type)
		}
	}

	if action.PullRequest == nil {
		logger.Warnf("no pull request spec was provided on gitops action. So no PR will be created")
		t.shouldCreatePR = false
	}

	if t.shouldCreatePR && action.Repo.Base == action.Repo.Branch {
		logger.Warnf("no base branch was provided on gitops action. So no PR will be created")
		t.shouldCreatePR = false
	}

	if t.shouldCreatePR {
		t.spec.PullRequest = &connectors.PullRequestTemplate{
			Base:   action.Repo.Base,
			Branch: action.Repo.Branch,
			Title:  action.PullRequest.Title,
			Tags:   action.PullRequest.Tags,
		}

		t.spec.PullRequest.Title, err = gomplate.RunTemplate(t.env.AsMap(), gomplate.Template{Template: action.PullRequest.Title})
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *GitOps) cloneRepo(ctx context.Context, action v1.GitOpsAction) (connectors.Connector, *gitv5.Worktree, error) {
	connector, workTree, err := git.Clone(ctx, t.spec)
	if err != nil {
		return nil, nil, err
	}

	return connector, workTree, nil
}

func (t *GitOps) applyPatches(ctx context.Context, action v1.GitOpsAction) error {
	for _, patch := range action.Patches {
		path, err := gomplate.RunTemplate(t.env.AsMap(), gomplate.Template{Template: patch.Path})
		if err != nil {
			return err
		}

		fullpath := filepath.Join(t.workTree.Filesystem.Root(), patch.Path)
		if patch.YQ != "" {
			script, err := gomplate.RunTemplate(t.env.AsMap(), gomplate.Template{Template: patch.YQ})
			if err != nil {
				return err
			}
			patch.YQ = script

			cmd := exec.Command("yq", "eval", "-i", patch.YQ, fullpath)
			if _, err := runCmd(cmd); err != nil {
				return err
			}

			if _, err := t.workTree.Add(path); err != nil {
				return err
			}
		}

		// TODO:
		// if patch.JQ != "" {
		// }
	}

	return nil
}

func (t *GitOps) modifyFiles(ctx context.Context, action v1.GitOpsAction) error {
	var err error

	for _, files := range action.Files {
		for path, content := range files {
			path, err = gomplate.RunTemplate(t.env.AsMap(), gomplate.Template{Template: path})
			if err != nil {
				return err
			}

			switch content {
			case "$delete":
				if _, err := t.workTree.Remove(path); err != nil {
					return err
				}

			default:
				templated, err := gomplate.RunTemplate(t.env.AsMap(), gomplate.Template{Template: content})
				if err != nil {
					return err
				}

				if err := os.WriteFile(filepath.Join(t.workTree.Filesystem.Root(), path), []byte(templated), os.ModePerm); err != nil {
					return err
				}

				if _, err := t.workTree.Add(path); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (t *GitOps) createPR(ctx context.Context, connector connectors.Connector, work *gitv5.Worktree) (int, error) {
	return git.OpenPR(ctx, connector, t.spec)
}
