package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

// resolveSkillPaths resolves local and git skill paths for the LLM skills middleware.
func resolveSkillPaths(ctx context.Context, skills []v1.AISkill) ([]string, error) {
	paths := make([]string, 0, len(skills))

	for i, skill := range skills {
		if strings.TrimSpace(skill.Path) == "" {
			return nil, fmt.Errorf("skill[%d]: path is required", i)
		}

		var root string
		if skill.Connection != "" {
			clonedRoot, err := cloneGitRepo(ctx, skill.Connection, skill.Branch)
			if err != nil {
				return nil, fmt.Errorf("skill[%d]: %w", i, err)
			}
			root = clonedRoot
		}

		resolved, err := resolveAndValidateSkillPath(root, skill.Path)
		if err != nil {
			return nil, fmt.Errorf("skill[%d] %q: %w", i, skill.Path, err)
		}
		paths = append(paths, resolved)
	}

	warnEmptySkillPaths(ctx, paths)

	return paths, nil
}

// resolveAndValidateSkillPath resolves a skill path to an absolute directory and
// validates it. The same constraints are applied to both git-backed and
// local-filesystem skills:
//
//   - symlink verification: the path is resolved through filepath.EvalSymlinks
//     so symlinked skill directories are followed to their real target.
//   - directory traversal validation: when root is non-empty (git-backed
//     skills), the path must be repo-relative and the resolved target must not
//     escape root. When root is empty (local-filesystem skills), absolute paths
//     are allowed and no traversal boundary is enforced, by design.
//   - the resolved path must be a directory.
func resolveAndValidateSkillPath(root, p string) (string, error) {
	var resolved string
	if root != "" {
		if filepath.IsAbs(p) {
			return "", fmt.Errorf("absolute paths are not allowed: %q", p)
		}
		joined := filepath.Join(root, filepath.Clean(p))
		var err error
		resolved, err = filepath.EvalSymlinks(joined)
		if err != nil {
			return "", fmt.Errorf("failed to resolve path %q: %w", p, err)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("path %q resolves outside the repository root", p)
		}
	} else {
		var err error
		resolved, err = filepath.EvalSymlinks(p)
		if err != nil {
			return "", fmt.Errorf("failed to resolve path %q: %w", p, err)
		}
	}

	fi, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to stat path %q: %w", p, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("path %q must be a directory containing skill subdirectories", p)
	}

	return resolved, nil
}

// warnEmptySkillPaths logs a warning for any resolved path that does not contain
// at least one skill subdirectory with a SKILL.md file. The genkit skills
// middleware silently skips such paths, which would otherwise hide a
// misconfiguration (e.g. pointing Path at a skill directory instead of its
// parent).
func warnEmptySkillPaths(ctx context.Context, paths []string) {
	for _, p := range paths {
		if !pathHasSkill(p) {
			ctx.Logger.Warnf("ai skill path %q contains no skill subdirectories with a SKILL.md file; skills will not be loaded from it", p)
		}
	}
}

// pathHasSkill reports whether the directory p contains at least one
// subdirectory with a SKILL.md file, matching the layout expected by the
// genkit skills middleware (p/<skill-name>/SKILL.md).
func pathHasSkill(p string) bool {
	entries, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if fi, err := os.Stat(filepath.Join(p, entry.Name(), "SKILL.md")); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}
