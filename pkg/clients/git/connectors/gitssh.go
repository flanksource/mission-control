package connectors

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/jenkins-x/go-scm/scm"
	"github.com/pkg/errors"
	ssh2 "golang.org/x/crypto/ssh"
)

type GitSSH struct {
	*scm.Client
	url  string
	auth transport.AuthMethod
}

func (g *GitSSH) OpenPullRequest(ctx context.Context, spec PullRequestTemplate) (int, error) {
	return 0, fmt.Errorf("open pull request  not implemented for git ssh")
}

func (g *GitSSH) ClosePullRequest(ctx context.Context, id int) error {
	return fmt.Errorf("close pull request  not implemented for git ssh")
}

func (g *GitSSH) Push(ctx context.Context, branch string) error {
	return fmt.Errorf("push not implemented for git ssh")
}

func (g *GitSSH) Clone(ctx context.Context, branch, local string) (billy.Filesystem, *git.Worktree, error) {
	// Filesystem abstraction based on memory
	fs := memfs.New()

	repo, err := git.Clone(memory.NewStorage(), fs, &git.CloneOptions{
		URL:      g.url,
		Progress: os.Stdout,
		Auth:     g.auth,
	})
	if err != nil {
		return nil, nil, err
	}

	work, err := repo.Worktree()
	if err != nil {
		return nil, nil, err
	}

	return fs, work, nil
}

func NewGitSSH(url, user string, privateKey []byte, password string) (Connector, error) {
	publicKeys, err := ssh.NewPublicKeys(user, privateKey, password)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create public keys")
	}
	publicKeys.HostKeyCallback = ssh2.InsecureIgnoreHostKey()

	github := &GitSSH{
		url:  url,
		auth: publicKeys,
	}
	return github, nil
}
