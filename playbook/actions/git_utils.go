package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/pkg/clients/git"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
)

// cloneGitRepo resolves a git connection and clones the repo, returning the worktree root path.
func cloneGitRepo(ctx context.Context, connectionRef, branch string) (string, error) {
	conn, err := pkgConnection.Get(ctx, connectionRef)
	if err != nil {
		return "", fmt.Errorf("failed to get git connection %q: %w", connectionRef, err)
	} else if conn == nil {
		return "", fmt.Errorf("git connection %q not found", connectionRef)
	}

	spec := &connectors.GitopsAPISpec{
		Repository: conn.URL,
		Base:       "main",
		Branch:     "main",
	}

	if branch != "" {
		spec.Base = branch
		spec.Branch = branch
	}

	switch conn.Type {
	case models.ConnectionTypeGithub, models.ConnectionTypeGitlab, models.ConnectionTypeAzureDevops:
		spec.AccessToken = conn.Password
	case models.ConnectionTypeHTTP:
		spec.User = conn.Username
		spec.Password = conn.Password
	case models.ConnectionTypeGit:
		spec.User = conn.Username
		spec.Password = conn.Password
		spec.SSHPrivateKey = conn.Certificate
		spec.SSHPrivateKeyPassword = conn.Password
	default:
		return "", fmt.Errorf("unsupported connection type %q", conn.Type)
	}

	_, workTree, err := git.Clone(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return workTree.Filesystem.Root(), nil
}

// safeResolvePath validates that filePath does not escape root via traversal.
func safeResolvePath(root, filePath string) (string, error) {
	if filepath.IsAbs(filePath) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", filePath)
	}

	joined := filepath.Join(root, filepath.Clean(filePath))
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", filePath, err)
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q resolves outside the repository root", filePath)
	}

	return resolved, nil
}

// safeReadFile validates that filePath does not escape root via traversal,
// then reads and returns the file content.
func safeReadFile(root, filePath string) ([]byte, error) {
	resolved, err := safeResolvePath(root, filePath)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(resolved)
}

// loadFileFromGit clones a git repo and reads a file at the given path.
func loadFileFromGit(ctx context.Context, connectionRef, filePath, branch string) (string, error) {
	root, err := cloneGitRepo(ctx, connectionRef, branch)
	if err != nil {
		return "", err
	}

	content, err := safeReadFile(root, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %q: %w", filePath, err)
	}

	return string(content), nil
}
