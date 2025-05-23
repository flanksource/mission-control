package connectors

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/utils"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/jenkins-x/go-scm/scm"
	"github.com/pkg/errors"
	ssh2 "golang.org/x/crypto/ssh"
)

type Git struct {
	*scm.Client
	repo *git.Repository
	url  string
	auth transport.AuthMethod
}

func (g *Git) OpenPullRequest(ctx context.Context, spec PullRequestTemplate) (*PullRequest, error) {
	return nil, fmt.Errorf("open pull request  not implemented for git ssh")
}

func (g *Git) ClosePullRequest(ctx context.Context, id int) error {
	return fmt.Errorf("close pull request  not implemented for git ssh")
}

func (g *Git) Push(ctx context.Context, branch string) error {
	if g.repo == nil {
		return errors.New("Need to clone first, before pushing ")
	}

	return g.repo.Push(&git.PushOptions{
		Auth:     g.auth,
		Progress: ctx.Logger.V(3),
	})
}

func (g *Git) Clone(ctx context.Context, branch, local string) (billy.Filesystem, *git.Worktree, error) {
	cloneDir, err := utils.CreateTempSubdir(".git-clones", "git-*")
	if err != nil {
		return nil, nil, err
	}

	repo, err := git.PlainCloneContext(ctx, cloneDir, false, &git.CloneOptions{
		URL:           g.url,
		Progress:      ctx.Logger.V(4),
		Auth:          g.auth,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
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

	if branch != local && local != "" {
		err := work.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(local),
			Create: true,
		})
		if err != nil {
			return nil, nil, err
		}
	}

	return osfs.New(cloneDir), work, nil
}

func NewGitSSH(url, user string, privateKey []byte, password string) (Connector, error) {
	publicKeys, err := ssh.NewPublicKeys(user, privateKey, password)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create public keys")
	}
	publicKeys.HostKeyCallback = ssh2.InsecureIgnoreHostKey()

	git := &Git{
		url:  url,
		auth: publicKeys,
	}
	return git, nil
}

func NewGitPassword(url, user string, password string) (Connector, error) {
	publicKeys := &http.BasicAuth{
		Username: user,
		Password: password,
	}

	git := &Git{
		url:  url,
		auth: publicKeys,
	}
	return git, nil
}
