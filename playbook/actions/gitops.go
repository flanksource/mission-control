package actions

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flanksource/commons/files"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/pkg/clients/git"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
	gitv5 "github.com/go-git/go-git/v5"
)

type GitOps struct {
	workTree       *gitv5.Worktree
	spec           *connectors.GitopsAPISpec
	logLines       []string
	shouldCreatePR bool
}

type GitOpsActionResult struct {
	CreatedPR string `json:"createdPR,omitempty"`
	Logs      string `json:"logs,omitempty"`
}

func (t *GitOps) log(msg string, args ...any) {
	t.logLines = append(t.logLines, fmt.Sprintf(msg, args...))
}

func (t *GitOps) logJSON(msg any) {
	b, _ := json.MarshalIndent(msg, "", "  ")
	t.logLines = append(t.logLines, string(b))
}

func (t *GitOps) Run(ctx context.Context, action v1.GitOpsAction) (*GitOpsActionResult, error) {
	var response GitOpsActionResult

	if len(action.Patches) == 0 && len(action.Files) == 0 {
		return nil, fmt.Errorf("no patches or files specified on gitops action")
	}

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

	if hash, err := git.CommitAndPush(ctx, connector, workTree, t.spec); err != nil {
		return nil, fmt.Errorf("failed to commit and push: %w", err)
	} else {
		t.log("committed(%s) and pushed changes", hash)
	}

	if t.shouldCreatePR {
		pr, err := t.createPR(ctx, connector, workTree)
		if err != nil {
			return nil, err
		}
		response.CreatedPR = pr.Link
		t.log("successfully created pull request")
		t.logJSON(pr)
	}
	response.Logs = strings.Join(t.logLines, "\n")

	return &response, nil
}

// generateSpec generates the spec for the git client from the action
func (t *GitOps) generateSpec(ctx context.Context, action v1.GitOpsAction) error {
	if action.Repo.Base == "" {
		action.Repo.Base = "main"
	}

	if action.Repo.Branch == "" {
		action.Repo.Branch = action.Repo.Base
	}

	t.spec = &connectors.GitopsAPISpec{
		Repository:        action.Repo.URL,
		Base:              action.Repo.Base,
		Branch:            action.Repo.Branch,
		CommitMsg:         action.Commit.Message,
		CommitAuthor:      action.Commit.AuthorName,
		CommitAuthorEmail: action.Commit.AuthorEmail,
	}

	if action.Repo.Connection != "" {
		conn, err := ctx.HydrateConnectionByURL(action.Repo.Connection)
		if err != nil {
			return err
		} else if conn == nil {
			return fmt.Errorf("connection %s not found", action.Repo.Connection)
		}

		switch conn.Type {
		case models.ConnectionTypeGithub, models.ConnectionTypeGitlab, models.ConnectionTypeAzureDevops:
			t.spec.AccessToken = conn.Password
			t.shouldCreatePR = true

		case models.ConnectionTypeHTTP:
			t.spec.User = conn.Username
			t.spec.Password = conn.Password

		case models.ConnectionTypeGit:
			t.spec.User = conn.Username
			t.spec.Password = conn.Password
			t.spec.SSHPrivateKey = conn.Certificate
			t.spec.SSHPrivateKeyPassword = conn.Password

		default:
			return fmt.Errorf("unsupported connection type: %s", conn.Type)
		}
	}

	if action.PullRequest == nil {
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
		fullpath := filepath.Join(t.workTree.Filesystem.Root(), patch.Path)
		paths, err := files.UnfoldGlobs(fullpath)
		if err != nil {
			return err
		}

		for _, path := range paths {
			relativePath, err := filepath.Rel(t.workTree.Filesystem.Root(), path)
			if err != nil {
				return fmt.Errorf("failed to get relative path: %w", err)
			}
			t.log("Patching %s", relativePath)

			if patch.YQ != "" {
				cmd := exec.Command("yq", "eval", "-i", patch.YQ, path)
				if res, err := runCmd(ctx, cmd); err != nil {
					return err
				} else if res.Error != nil {
					return res.Error
				} else {
					t.log(res.Stdout)
				}

				if _, err := t.workTree.Add(relativePath); err != nil {
					return err
				}
			}

			// TODO:
			// if patch.JQ != "" {
			// }
		}
	}

	return nil
}

func (t *GitOps) modifyFiles(ctx context.Context, action v1.GitOpsAction) error {
	for _, f := range action.Files {
		fullpath := filepath.Join(t.workTree.Filesystem.Root(), f.Path)
		paths, err := files.UnfoldGlobs(fullpath)
		if err != nil {
			return err
		}

		for _, path := range paths {
			relativePath, err := filepath.Rel(t.workTree.Filesystem.Root(), path)
			if err != nil {
				return fmt.Errorf("failed to get relative path: %w", err)
			}

			switch f.Content {
			case "$delete":
				t.log("Deleting file %s", relativePath)
				if _, err := t.workTree.Remove(relativePath); err != nil {
					return fmt.Errorf("failed to delete file(%s): %w", relativePath, err)
				}

			default:
				t.log("Creating file %s", relativePath)
				if err := os.WriteFile(path, []byte(f.Content), os.ModePerm); err != nil {
					return fmt.Errorf("failed to create file(%s): %w", relativePath, err)
				}

				if _, err := t.workTree.Add(relativePath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (t *GitOps) createPR(ctx context.Context, connector connectors.Connector, work *gitv5.Worktree) (*connectors.PullRequest, error) {
	return git.OpenPR(ctx, connector, t.spec)
}
