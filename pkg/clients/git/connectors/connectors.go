package connectors

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/samber/lo"

	"github.com/flanksource/commons/logger"
	"github.com/go-git/go-billy/v5"
	git "github.com/go-git/go-git/v5"
	"github.com/jenkins-x/go-scm/scm"
)

const (
	ServiceGithub = "github"
	ServiceGitlab = "gitlab"
	ServiceAzure  = "azure"
)

// GitopsAPISpec defines the desired state of GitopsAPI
type GitopsAPISpec struct {
	// Service where the repository is hosted
	Service string

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

	User     string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	Force bool `json:"force,omitempty"`

	AccessToken string `json:"access_token,omitempty"`

	// For SSH repositories the secret must contain SSH_PRIVATE_KEY, SSH_PRIVATE_KEY_PASSORD
	SSHPrivateKey         string `json:"ssh_private_key,omitempty"`
	SSHPrivateKeyPassword string `json:"ssh_private_key_password,omitempty"`
}

func (g GitopsAPISpec) GetContext() map[string]any {
	return map[string]any{
		"service":               g.Service,
		"repository":            g.Repository,
		"base":                  g.Base,
		"branch":                g.Branch,
		"author":                g.CommitAuthor,
		"author.email":          g.CommitAuthorEmail,
		"user":                  logger.PrintableSecret(g.User),
		"password":              logger.PrintableSecret(g.Password),
		"accessToken":           logger.PrintableSecret(g.AccessToken),
		"sshPrivateKey":         logger.PrintableSecret(g.SSHPrivateKey),
		"sshPrivateKeyPassword": logger.PrintableSecret(g.SSHPrivateKeyPassword),
	}
}

type PullRequest scm.PullRequest

func (t *PullRequest) AsMap() (map[string]any, error) {
	m := make(map[string]any)
	b, err := json.Marshal(&t)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}

	return m, nil
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
	OpenPullRequest(ctx context.Context, spec PullRequestTemplate) (*PullRequest, error)
	ClosePullRequest(ctx context.Context, id int) error
}

func inferServiceType(gitConfig *GitopsAPISpec) string {
	if gitConfig.Service != "" {
		return gitConfig.Service
	}

	repo := gitConfig.Repository
	if strings.Contains(repo, "github.com") {
		return ServiceGithub
	}
	if strings.Contains(repo, "gitlab.com") {
		return ServiceGitlab
	}

	if strings.Contains(repo, "azure.com") {
		if _, _, _, ok := parseAzureDevopsRepo(repo); ok {
			return ServiceAzure
		}
	}

	return ""
}

func NewConnector(gitConfig *GitopsAPISpec) (Connector, error) {
	token := lo.CoalesceOrEmpty(gitConfig.AccessToken, gitConfig.Password, gitConfig.User)

	service := inferServiceType(gitConfig)
	if service != "" {
		return NewAccessTokenClient(gitConfig.Repository, service, token)
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

func parseRepoURL(repoURL string) (owner string, repo string, err error) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", "", err
	}

	path := strings.TrimSuffix(parsed.Path, ".git")
	path = strings.TrimPrefix(path, "/")
	paths := strings.Split(path, "/")
	return strings.Join(paths[:len(paths)-1], "/"), paths[len(paths)-1], nil
}
