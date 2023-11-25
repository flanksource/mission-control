package connectors

import (
	"context"
	"strings"

	"github.com/go-git/go-billy/v5"
	git "github.com/go-git/go-git/v5"
	"github.com/pkg/errors"
)

// GitopsAPISpec defines the desired state of GitopsAPI
type GitopsAPISpec struct {
	// The repository URL, can be a HTTP or SSH address.
	Repository string `json:"repository,omitempty"`

	// Base to clone
	Base string `json:"base,omitempty"`

	// Branch to checkout after clone
	Branch string `json:"branch,omitempty"`

	CommitAuthor      string `json:"user,omitempty"`
	CommitAuthorEmail string `json:"email,omitempty"`
	CommitMsg         string `json:"commit_msg,omitempty"`

	// Open a new Pull request from the branch back to the base
	PullRequest *PullRequestTemplate `json:"pull_request,omitempty"`

	User     string `json:"auth_user,omitempty"`
	Password string `json:"password,omitempty"`

	// For Github repositories it must contain GITHUB_TOKEN
	GITHUB_TOKEN string `json:"github_token,omitempty"`

	// For SSH repositories the secret must contain SSH_PRIVATE_KEY, SSH_PRIVATE_KEY_PASSORD
	SSH_PRIVATE_KEY         string `json:"ssh_private_key,omitempty"`
	SSH_PRIVATE_KEY_PASSORD string `json:"ssh_private_key_password,omitempty"`
}

type PullRequestTemplate struct {
	Body      string   `json:"body,omitempty"`
	Title     string   `json:"title,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Tags      []string `json:"tags,omitempty"`

	// The branch to use as a baseline for the new branch, defaults to master
	Base string `json:"base,omitempty"`
	// The branch to push updates back to, defaults to master
	Branch string `json:"branch,omitempty"`
}

type Connector interface {
	Clone(ctx context.Context, branch, local string) (billy.Filesystem, *git.Worktree, error)
	Push(ctx context.Context, branch string) error
	OpenPullRequest(ctx context.Context, spec PullRequestTemplate) (int, error)
	ClosePullRequest(ctx context.Context, id int) error
}

func NewConnector(gitConfig *GitopsAPISpec) (Connector, error) {
	if strings.HasPrefix(gitConfig.Repository, "https://github.com/") {
		path := gitConfig.Repository[19:]
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return nil, errors.Errorf("invalid repository url: %s", gitConfig.Repository)
		}
		owner := parts[0]
		repoName := parts[1]
		repoName = strings.TrimSuffix(repoName, ".git")
		githubToken := gitConfig.GITHUB_TOKEN
		return NewGithub(owner, repoName, githubToken)
	} else if strings.HasPrefix(gitConfig.Repository, "ssh://") {
		sshURL := gitConfig.Repository[6:]
		user := strings.Split(sshURL, "@")[0]

		privateKey := gitConfig.SSH_PRIVATE_KEY
		password := gitConfig.SSH_PRIVATE_KEY_PASSORD
		return NewGitSSH(sshURL, user, []byte(privateKey), password)
	} else {
		return NewGitPassword(gitConfig.Repository, gitConfig.User, gitConfig.Password)
	}
}
