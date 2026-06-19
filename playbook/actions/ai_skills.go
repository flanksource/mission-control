package actions

import (
	"fmt"
	"os"
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

		if _, err := os.Stat(skill.Path); err != nil {
			return nil, fmt.Errorf("skill[%d] %q: %w", i, skill.Path, err)
		}
		paths = append(paths, skill.Path)
	}

	return paths, nil
}
