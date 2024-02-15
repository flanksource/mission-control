package connectors

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/jenkins-x/go-scm/scm"
	"github.com/jenkins-x/go-scm/scm/factory"
	"github.com/pkg/errors"
)

type GitAccessTokenClient struct {
	// service is the name of the git service
	service string

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
	scmClient, err := factory.NewClient(service, "", accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create git client with access token: %v", err)
	}

	client := &GitAccessTokenClient{
		service:    service,
		scm:        scmClient,
		owner:      owner,
		repoName:   repoName,
		repository: owner + "/" + repoName,
		auth:       &http.BasicAuth{Password: accessToken, Username: accessToken},
	}
	return client, nil
}

func (g *GitAccessTokenClient) Push(ctx context.Context, branch string) error {
	if g.repo == nil {
		return errors.New("Need to clone first, before pushing ")
	}

	if err := g.repo.Push(&git.PushOptions{
		Auth:     g.auth,
		Progress: os.Stdout,
	}); err != nil {
		return err
	}
	return nil
}

func (g *GitAccessTokenClient) OpenPullRequest(ctx context.Context, spec PullRequestTemplate) (*PullRequest, error) {
	if spec.Title == "" {
		spec.Title = spec.Branch
	}
	pr, _, err := g.scm.PullRequests.Create(ctx, g.repository, &scm.PullRequestInput{
		Title: spec.Title,
		Body:  spec.Body,
		Head:  spec.Branch,
		Base:  spec.Base,
	})

	if err != nil {
		return nil, errors.Wrapf(err, "failed to create pr repo=%s title=%s, head=%s base=%s", g.repository, spec.Title, spec.Branch, spec.Base)
	}

	if len(spec.Reviewers) > 0 {
		if _, err := g.scm.PullRequests.RequestReview(ctx, g.repository, pr.Number, spec.Reviewers); err != nil {
			return nil, err
		}
	}

	if len(spec.Assignees) > 0 {
		if _, err := g.scm.PullRequests.AssignIssue(ctx, g.repository, pr.Number, spec.Assignees); err != nil {
			return nil, err
		}
	}

	return (*PullRequest)(pr), nil
}

func (g *GitAccessTokenClient) ClosePullRequest(ctx context.Context, id int) error {
	if _, err := g.scm.PullRequests.Close(ctx, g.repository, id); err != nil {
		return errors.Wrap(err, "failed to close github pull request")
	}

	return nil
}

func (g *GitAccessTokenClient) Clone(ctx context.Context, branch, local string) (billy.Filesystem, *git.Worktree, error) {
	dir, _ := os.MkdirTemp("", fmt.Sprintf("%s-*", g.service))
	url := fmt.Sprintf("https://%s.com/%s/%s.git", g.service, g.owner, g.repoName)
	if g.service == "azure" {
		url = fmt.Sprintf("https://dev.azure.com/%s/_git/%s", g.owner, g.repoName)
	}

	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		URL:           url,
		Progress:      io.Discard,
		Auth:          g.auth,
		Depth:         1,
	})
	if err != nil {
		return nil, nil, err
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
			return nil, nil, err
		}
	}

	return osfs.New(dir), work, nil
}
