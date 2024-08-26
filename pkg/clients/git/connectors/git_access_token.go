package connectors

import (
	"fmt"
	"os"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/samber/oops"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/jenkins-x/go-scm/scm"
	"github.com/jenkins-x/go-scm/scm/factory"
	"github.com/pkg/errors"
)

type GitAccessTokenClient struct {
	// service is the name of the git service
	service    string
	scm        *scm.Client
	repo       *git.Repository
	auth       transport.AuthMethod
	owner      string
	repoName   string
	repository string
}

// NewAccessTokenClient is a generic git client that can communicate with
// git services supporting access tokens. eg Github, GitLab & Azure Devops (WIP)
func NewAccessTokenClient(service, owner, repoName, accessToken string) (Connector, error) {

	logger.Infof("Creating %s client for %s/%s using access token: %s", service, owner, repoName, logger.PrintableSecret(accessToken))
	scmClient, err := factory.NewClient(service, "", accessToken)

	scmClient.Client.Transport = logger.NewHttpLogger(logger.GetLogger("git"), scmClient.Client.Transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create git client with access token: %v", err)
	}
	// scmClient.
	client := &GitAccessTokenClient{
		service:    service,
		scm:        scmClient,
		owner:      owner,
		repoName:   repoName,
		repository: owner + "/" + repoName,
		auth:       &http.BasicAuth{Password: accessToken, Username: "token"},
	}
	return client, nil
}

func (g *GitAccessTokenClient) Push(ctx context.Context, branch string) error {
	if g.repo == nil {
		return errors.New("Need to clone first, before pushing ")
	}

	if err := g.repo.Push(&git.PushOptions{
		Auth:     g.auth,
		Force:    true,
		Progress: ctx.Logger.V(3),
	}); err != nil {
		return oops.Wrapf(err, "failed to push branch %s", branch)
	}
	return nil
}

func (g *GitAccessTokenClient) OpenPullRequest(ctx context.Context, spec PullRequestTemplate) (*PullRequest, error) {
	if spec.Title == "" {
		spec.Title = spec.Branch
	}

	ctx = ctx.WithLoggingValues("title", spec.Title, "from", spec.Branch, "into", spec.Base)
	ctx.Tracef("Creating PR %s %s into %s", spec.Title, spec.Branch, spec.Base)

	pr, _, err := g.scm.PullRequests.Create(ctx, g.repository, &scm.PullRequestInput{
		Title: spec.Title,
		Body:  spec.Body,
		Head:  spec.Branch,
		Base:  spec.Base,
	})
	ctx = ctx.WithLoggingValues("pr", pr.Number)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to create pr repo=%s title=%s, head=%s base=%s", g.repository, spec.Title, spec.Branch, spec.Base)
	}

	if len(spec.Reviewers) > 0 {
		ctx.Tracef("Requesting review from %v for PR #%d", spec.Reviewers, pr.Number)
		if _, err := g.scm.PullRequests.RequestReview(ctx, g.repository, pr.Number, spec.Reviewers); err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
	}

	if len(spec.Assignees) > 0 {
		ctx.Tracef("Assigning %s to PR #%d", spec.Assignees, pr.Number)
		if _, err := g.scm.PullRequests.AssignIssue(ctx, g.repository, pr.Number, spec.Assignees); err != nil {
			return nil, oops.Wrap(err)
		}
	}

	return (*PullRequest)(pr), nil
}

func (g *GitAccessTokenClient) ClosePullRequest(ctx context.Context, id int) error {
	ctx.Tracef("Closing pull request %s/%d", g.repository, id)
	if _, err := g.scm.PullRequests.Close(ctx, g.repository, id); err != nil {
		return oops.Wrapf(err, "failed to close github pull request")
	}

	return nil
}

func (g *GitAccessTokenClient) Clone(ctx context.Context, branch, local string) (billy.Filesystem, *git.Worktree, error) {
	dir, _ := os.MkdirTemp("", fmt.Sprintf("%s-*", g.service))
	url := fmt.Sprintf("https://%s.com/%s/%s.git", g.service, g.owner, g.repoName)
	transport.UnsupportedCapabilities = nil // reset the global list of unsupported capabilities

	if g.service == "azure" {
		url = fmt.Sprintf("https://dev.azure.com/%s/_git/%s", g.owner, g.repoName)
		transport.UnsupportedCapabilities = []capability.Capability{
			capability.ThinPack,
		}
	}

	ctx.Logger.V(9).Infof("Cloning %s@%s", url, branch)
	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		URL:           url,
		Progress:      ctx.Logger.V(4).WithFilter("Compressing objects", "Counting objects"),
		Auth:          g.auth,
		Depth:         1,
	})
	if err != nil {
		return nil, nil, oops.Wrap(err)
	}
	g.repo = repo

	work, err := repo.Worktree()
	if err != nil {
		return nil, nil, err
	}
	if branch != local {
		err := work.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(local),
			Create: true,
		})
		if err != nil {
			return nil, nil, oops.Wrap(err)
		}
	}

	return osfs.New(dir), work, nil
}
