package connectors

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-git/go-billy/v5"
	git "github.com/go-git/go-git/v5"
	"github.com/jenkins-x/go-scm/scm"
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

	AccessToken string `json:"access_token,omitempty"`

	// For SSH repositories the secret must contain SSH_PRIVATE_KEY, SSH_PRIVATE_KEY_PASSORD
	SSHPrivateKey         string `json:"ssh_private_key,omitempty"`
	SSHPrivateKeyPassword string `json:"ssh_private_key_password,omitempty"`
}

type PullRequest scm.PullRequest

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
	OpenPullRequest(ctx context.Context, spec PullRequestTemplate) (*PullRequest, error)
	ClosePullRequest(ctx context.Context, id int) error
}

func NewConnector(gitConfig *GitopsAPISpec) (Connector, error) {
	if strings.HasPrefix(gitConfig.Repository, "https://github.com/") {
		path := gitConfig.Repository[19:]
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return nil, errors.Errorf("invalid GitHub repository url: %s. expected: https://github.com/<owner>/<repo>", gitConfig.Repository)
		}
		owner := parts[0]
		repoName := parts[1]
		repoName = strings.TrimSuffix(repoName, ".git")
		return NewAccessTokenClient("github", owner, repoName, gitConfig.AccessToken)
	} else if strings.HasPrefix(gitConfig.Repository, "https://gitlab.com") {
		path := gitConfig.Repository[19:]
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return nil, errors.Errorf("invalid GitLab repository url: %s. expected: https://gitlab.com/<owner>/<repo>", gitConfig.Repository)
		}
		owner := parts[0]
		repoName := parts[1]
		repoName = strings.TrimSuffix(repoName, ".git")
		return NewAccessTokenClient("gitlab", owner, repoName, gitConfig.AccessToken)
	} else if azureOrg, azureProject, azureRepo, ok := parseAzureDevopsRepo(gitConfig.Repository); ok {
		// Note: go-git client doesn't support azure devops repository as of now
		// GH Issue: https://github.com/go-git/go-git/issues/64
		return NewAccessTokenClient("azure", fmt.Sprintf("%s/%s", azureOrg, azureProject), azureRepo, gitConfig.AccessToken)
	} else if strings.HasPrefix(gitConfig.Repository, "ssh://") {
		sshURL := gitConfig.Repository[6:]
		user := strings.Split(sshURL, "@")[0]

		privateKey := gitConfig.SSHPrivateKey
		password := gitConfig.SSHPrivateKeyPassword
		return NewGitSSH(sshURL, user, []byte(privateKey), password)
	} else {
		return NewGitPassword(gitConfig.Repository, gitConfig.User, gitConfig.Password)
	}
}

var azureDevopsRepoURLRegexp = regexp.MustCompile(`^https:\/\/[a-zA-Z0-9_-]+@dev\.azure\.com\/([a-zA-Z0-9_-]+)\/([a-zA-Z0-9_-]+)\/_git\/([a-zA-Z0-9_-]+)`)

func parseAzureDevopsRepo(url string) (org, project, repo string, ok bool) {
	matches := azureDevopsRepoURLRegexp.FindStringSubmatch(url)
	if len(matches) != 4 {
		return "", "", "", false
	}

	return matches[1], matches[2], matches[3], true
}
