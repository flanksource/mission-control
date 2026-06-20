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

		if skill.Connection != "" {
			root, err := cloneGitRepo(ctx, skill.Connection, skill.Branch)
			if err != nil {
				return nil, fmt.Errorf("skill[%d]: %w", i, err)
			}

			resolved, err := safeResolvePath(root, skill.Path)
			if err != nil {
				return nil, fmt.Errorf("skill[%d] %q: %w", i, skill.Path, err)
			}

			paths = append(paths, resolved)
			continue
		}

		fi, err := os.Stat(skill.Path)
		if err != nil {
			return nil, fmt.Errorf("skill[%d] %q: %w", i, skill.Path, err)
		} else if !fi.IsDir() {
			return nil, fmt.Errorf("skill[%d] %q: path must be a directory containing skill subdirectories", i, skill.Path)
		}

		paths = append(paths, skill.Path)
	}

	warnEmptySkillPaths(ctx, paths)

	return paths, nil
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
