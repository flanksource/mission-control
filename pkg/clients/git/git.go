package git

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
	gitv5 "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type GitopsAPISpec = connectors.GitopsAPISpec

func Clone(ctx context.Context, spec *GitopsAPISpec) (connectors.Connector, *gitv5.Worktree, error) {
	connector, err := connectors.NewConnector(spec)
	if err != nil {
		return nil, nil, ctx.Oops().Wrap(err)
	}

	ctx.Logger.V(4).Infof("cloning %s", logger.Pretty(spec.GetContext()))
	fs, work, err := connector.Clone(ctx, spec.Base, spec.Branch)
	if err != nil {
		return nil, nil, ctx.Oops().Wrapf(err, "failed to clone repo %s (remoteBranch: %s, localBranch: %s)", spec.Repository, spec.Base, spec.Branch)
	}
	ctx.Tracef("successfully cloned remote: %s to local: %s", spec.Base, fs.Root())

	return connector, work, nil
}

func CommitAndPush(ctx context.Context, connector connectors.Connector, work *gitv5.Worktree, spec *GitopsAPISpec) (string, error) {
	hash, err := createCommit(ctx, work, spec.CommitMsg, spec.CommitAuthor, spec.CommitAuthorEmail)
	if err != nil {
		return "", ctx.Oops().Wrap(err)
	}

	ctx.Tracef("committed %s to local repo", hash)
	return hash, connector.Push(ctx, spec.Branch)
}

func OpenPR(ctx context.Context, connector connectors.Connector, spec *GitopsAPISpec) (*connectors.PullRequest, error) {
	pr, err := connector.OpenPullRequest(ctx, *spec.PullRequest)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	return pr, nil
}

func createCommit(ctx context.Context, work *gitv5.Worktree, message, author, email string) (string, error) {
	signature := &object.Signature{
		Name:  author,
		Email: email,
		When:  time.Now(),
	}
	_hash, err := work.Commit(message, &gitv5.CommitOptions{
		Author: signature,
		All:    true,
	})

	if err != nil {
		return "", ctx.Oops().Wrap(err)
	}
	return _hash.String(), nil
}
