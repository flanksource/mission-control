package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/flanksource/commons/files"
	"github.com/flanksource/commons/logger"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shell"
	gitv5 "github.com/go-git/go-git/v5"
	"github.com/samber/lo"
	"github.com/samber/oops"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/pkg/clients/git"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
)

type GitOps struct {
	context.Context
	workTree       *gitv5.Worktree
	spec           *connectors.GitopsAPISpec
	logLines       []string
	shouldCreatePR bool
}

type Link struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
	Icon string `json:"icon,omitempty"`
}

type GitOpsActionResult struct {
	Links []Link         `json:"links,omitempty"`
	Logs  string         `json:"logs,omitempty"`
	PR    map[string]any `json:"pr,omitempty"`
}

func (t *GitOps) log(msg string, args ...any) {
	t.Logger.V(3).Infof(msg, args...)
	msg = fmt.Sprintf("%s %s", time.Now().Format(time.RFC3339), msg)
	t.logLines = append(t.logLines, fmt.Sprintf(msg, args...))
}

// DeleteFileDirective is a special path value that indicates a file should be deleted
const DeleteFileDirective = "$delete"

var blacklistedPathSymbols = regexp.MustCompile(`[${}[\]?*:<>|]`)

func (t *GitOps) Run(ctx context.Context, action v1.GitOpsAction) (*GitOpsActionResult, error) {
	var response GitOpsActionResult

	if len(action.Patches) == 0 && len(action.Files) == 0 {
		return nil, ctx.Oops().Errorf("no patches or files specified on gitops action")
	}

	if err := t.validatePaths(ctx, action); err != nil {
		return nil, err
	}

	if err := t.generateSpec(ctx, action); err != nil {
		return nil, err
	}
	ctx = ctx.WithAppendObject(t.spec)

	connector, workTree, err := t.cloneRepo(ctx)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to clone repo")
	}
	t.workTree = workTree

	if err := t.applyPatches(ctx, action); err != nil {
		return nil, oops.Wrapf(err, "failed to apply patches")
	}

	if err := t.modifyFiles(ctx, action); err != nil {
		return nil, oops.Wrapf(err, "failed to modify files")
	}

	if hash, err := git.CommitAndPush(ctx, connector, workTree, t.spec); err != nil {
		return nil, oops.Wrapf(err, "failed to commit and push")
	} else {
		t.log("committed(%s) and pushed changes", hash)
	}

	if t.shouldCreatePR {
		t.log("creating pull request: %s\nbranch: %s\nauthor: %s", t.spec.Repository, t.spec.Branch, t.spec.CommitAuthor)

		pr, err := t.createPR(ctx, connector)
		if err != nil {
			return nil, ctx.Oops().Wrap(err)
		}

		if _pr, err := pr.AsMap(); err != nil {
			return nil, err
		} else {
			response.PR = _pr
		}

		response.Links = append(response.Links, Link{
			ID:   pr.Number,
			Icon: "pr",
			Name: pr.Title,
			URL:  pr.Link,
		})
		t.log("created pull request: %s", pr.Link)
	}

	response.Logs = strings.Join(t.logLines, "\n")

	return &response, nil
}

func (t *GitOps) validatePaths(ctx context.Context, action v1.GitOpsAction) error {
	for _, file := range action.Files {
		if file.Path == DeleteFileDirective {
			continue
		}

		if blacklistedPathSymbols.MatchString(file.Path) {
			return ctx.Oops().Errorf("path %s contains illegal characters", file.Path)
		}
	}

	for _, patch := range action.Patches {
		if blacklistedPathSymbols.MatchString(patch.Path) {
			return ctx.Oops().Errorf("path %s contains illegal characters", patch.Path)
		}
	}

	return nil
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
		Force:             action.Repo.Force,
		Repository:        action.Repo.URL,
		Base:              action.Repo.Base,
		Branch:            action.Repo.Branch,
		CommitMsg:         action.Commit.Message,
		CommitAuthor:      action.Commit.AuthorName,
		CommitAuthorEmail: action.Commit.AuthorEmail,
	}

	if action.Repo.Connection != "" {
		conn, err := pkgConnection.Get(ctx, action.Repo.Connection)
		if err != nil {
			return ctx.Oops().Wrap(err)
		} else if conn == nil {
			return ctx.Oops().Errorf("connection %s not found", action.Repo.Connection)
		}

		switch conn.Type {
		case models.ConnectionTypeGithub, models.ConnectionTypeGitlab, models.ConnectionTypeAzureDevops:
			ctx.Logger.V(6).Infof("Using %s authentication token %v", conn.Type, logger.PrintableSecret(conn.Password))
			t.spec.AccessToken = conn.Password
			t.shouldCreatePR = true
			switch conn.Type {
			case models.ConnectionTypeGithub:
				t.spec.Service = connectors.ServiceGithub
			case models.ConnectionTypeGitlab:
				t.spec.Service = connectors.ServiceGitlab
			case models.ConnectionTypeAzureDevops:
				t.spec.Service = connectors.ServiceAzure
			}

		case models.ConnectionTypeHTTP:
			ctx.Logger.V(6).Infof("Using http basic auth %s:%s", logger.PrintableSecret(conn.Username), logger.PrintableSecret(conn.Password))

			t.spec.User = conn.Username
			t.spec.Password = conn.Password

		case models.ConnectionTypeGit:
			ctx.Logger.V(6).Infof("Using git:// user=%s key=%s password=%s", logger.PrintableSecret(conn.Username), logger.PrintableSecret(conn.Certificate), logger.PrintableSecret(conn.Password))

			t.spec.User = conn.Username
			t.spec.Password = conn.Password
			t.spec.SSHPrivateKey = conn.Certificate
			t.spec.SSHPrivateKeyPassword = conn.Password

		default:
			return ctx.Oops().Errorf("unsupported connection type: %s", conn.Type)
		}
	}

	var err error
	if !action.Repo.Username.IsEmpty() {
		t.spec.User, err = ctx.GetEnvValueFromCache(action.Repo.Username, ctx.GetNamespace())
		if err != nil {
			return err
		}

	}
	if !action.Repo.Password.IsEmpty() {
		t.spec.Password, err = ctx.GetEnvValueFromCache(action.Repo.Password, ctx.GetNamespace())
		if err != nil {
			return err
		}
	}

	t.shouldCreatePR = action.PullRequest != nil
	if action.PullRequest == nil {
		ctx.Logger.V(3).Infof("Skipping PR creation, no pull request details provided")
	}

	if t.shouldCreatePR && action.Repo.Base == action.Repo.Branch {
		ctx.Warnf("no base branch was provided on gitops action. So no PR will be created")
		t.shouldCreatePR = false
	}

	if t.shouldCreatePR {

		ctx.Logger.V(3).Infof("Will create a PR from %s -> %s", action.Repo.Branch, action.Repo.Base)
		t.spec.PullRequest = &connectors.PullRequestTemplate{
			Base:   action.Repo.Base,
			Branch: action.Repo.Branch,
			Title:  action.PullRequest.Title,
			Tags:   action.PullRequest.Tags,
		}
	}

	return nil
}

func (t *GitOps) cloneRepo(ctx context.Context) (connectors.Connector, *gitv5.Worktree, error) {
	connector, workTree, err := git.Clone(ctx, t.spec)
	if err != nil {
		return nil, nil, err
	}

	return connector, workTree, nil
}

func (t *GitOps) applyPatches(ctx context.Context, action v1.GitOpsAction) error {
	for _, patch := range action.Patches {
		fullpath := filepath.Join(t.workTree.Filesystem.Root(), patch.Path)
		paths, err := files.DoubleStarGlob(fullpath)
		if err != nil {
			return err
		}

		for _, path := range paths {
			relativePath, err := filepath.Rel(t.workTree.Filesystem.Root(), path)
			if err != nil {
				return ctx.Oops().Wrap(err)
			}

			if _, err := os.Stat(path); os.IsNotExist(err) {
				return ctx.Oops().Errorf("%s does not exist", relativePath)
			}
			t.log("Patching %s", relativePath)

			if patch.YQ != "" {
				res, err := shell.Run(ctx, shell.Exec{Script: fmt.Sprintf("yq eval -i '%s' %s", patch.YQ, path)})
				if err != nil {
					return err
				}

				if res.ExitCode != 0 {
					return ctx.Oops().
						With("path", relativePath, "yq", patch.YQ).
						Errorf("yq: %s", lo.CoalesceOrEmpty(res.Stderr, res.Stdout, fmt.Sprintf("exit code %d ", res.ExitCode)))
				}
				if res != nil && res.Stderr != "" {
					t.log(res.Stderr)
				}
				if res != nil && res.Stdout != "" {
					t.log(res.Stdout)
				}

				if _, err := t.workTree.Add(relativePath); err != nil {
					return err
				}
			} else if patch.JQ != "" {
				res, err := shell.Run(ctx, shell.Exec{Script: fmt.Sprintf("jq '%s' %s", patch.JQ, path)})
				if err != nil {
					return err
				}

				if res.ExitCode != 0 {
					return ctx.Oops().
						With("path", relativePath, "jq", patch.JQ).
						Errorf("jq: %s", lo.CoalesceOrEmpty(res.Stderr, res.Stdout, fmt.Sprintf("exit code %d ", res.ExitCode)))
				}

				if res != nil && res.Stderr != "" {
					t.log(res.Stderr)
				}
				if res != nil && res.Stdout != "" {
					t.log(res.Stdout)
				}

				if _, err := t.workTree.Add(relativePath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (t *GitOps) modifyFiles(ctx context.Context, action v1.GitOpsAction) error {
	for _, f := range action.Files {
<<<<<<< HEAD
		paths, err := files.DoubleStarGlob(t.workTree.Filesystem.Root(), []string{f.Path})
=======
		fullpath := filepath.Join(t.workTree.Filesystem.Root(), f.Path)
		paths, err := files.DoubleStarGlob(fullpath)
>>>>>>> 314236a (chore: update dependencies + lint errors)
		if err != nil {
			return err
		}

		for _, path := range paths {
			relativePath, err := filepath.Rel(t.workTree.Filesystem.Root(), path)
			if err != nil {
				return ctx.Oops().Wrap(err)
			}

			switch f.Content {
			case DeleteFileDirective:
				if _, err := t.workTree.Filesystem.Stat(relativePath); os.IsNotExist(err) {
					t.log("File does not exist, skipping delete: %s", relativePath)
				} else if err != nil {
					return err
				} else {
					t.log("Deleting file %s", relativePath)
					if _, err := t.workTree.Remove(relativePath); err != nil {
						return ctx.Oops().Wrap(err)
					}
				}

			default:
				t.log("Creating file %s", relativePath)
				_ = t.workTree.Filesystem.MkdirAll(filepath.Dir(relativePath), 0600)
				if err := os.WriteFile(path, []byte(f.Content), os.ModePerm); err != nil {
					return ctx.Oops().Wrap(err)
				}

				if _, err := t.workTree.Add(relativePath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (t *GitOps) createPR(ctx context.Context, connector connectors.Connector) (*connectors.PullRequest, error) {
	return git.OpenPR(ctx, connector, t.spec)
}
