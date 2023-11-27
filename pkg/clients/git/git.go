package git

import (
	"context"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
	gitv5 "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type GitopsAPISpec = connectors.GitopsAPISpec

func Clone(ctx context.Context, spec *GitopsAPISpec) (connectors.Connector, *gitv5.Worktree, error) {
	connector, err := connectors.NewConnector(spec)
	if err != nil {
		return nil, nil, err
	}

	fs, work, err := connector.Clone(ctx, spec.Base, spec.Branch)
	if err != nil {
		return nil, nil, err
	}
	logger.Infof("Successfully cloned %s to %s", spec.Base, fs.Root())

	return connector, work, nil
}

func CommitAndPush(ctx context.Context, connector connectors.Connector, work *gitv5.Worktree, spec *GitopsAPISpec) (string, error) {
	hash, err := createCommit(work, spec.CommitMsg, spec.CommitAuthor, spec.CommitAuthorEmail)
	if err != nil {
		return "", err
	}

	return hash, connector.Push(ctx, spec.Branch)
}

func OpenPR(ctx context.Context, connector connectors.Connector, spec *GitopsAPISpec) (int, error) {
	pr, err := connector.OpenPullRequest(ctx, *spec.PullRequest)
	if err != nil {
		return 0, err
	}

	return pr, nil
}

func createCommit(work *gitv5.Worktree, message, author, email string) (hash string, err error) {
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
		return
	}
	hash = _hash.String()
	return
}
