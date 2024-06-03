package playbook

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type GitOpsEnvKustomize struct {
	Path string `json:"path"`
}

func (t *GitOpsEnvKustomize) AsMap() map[string]any {
	return map[string]any{
		"path": t.Path,
	}
}

type GitOpsEnvGit struct {
	File   string `json:"file"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
}

func (t *GitOpsEnvGit) AsMap() map[string]any {
	return map[string]any{
		"file":   t.File,
		"url":    t.URL,
		"branch": t.Branch,
	}
}

type GitOpsEnv struct {
	Git       GitOpsEnvGit       `json:"git"`
	Kustomize GitOpsEnvKustomize `json:"kustomize"`
}

func (t *GitOpsEnv) AsMap() map[string]any {
	return map[string]any{
		"git":       t.Git.AsMap(),
		"kustomize": t.Kustomize.AsMap(),
	}
}

func getGitOpsTemplateVars(ctx context.Context, run models.PlaybookRun, actions []v1.PlaybookAction) (*GitOpsEnv, error) {
	if run.ConfigID == nil {
		return nil, nil
	}

	var hasGitOpsAction bool
	for _, action := range actions {
		if action.GitOps != nil {
			hasGitOpsAction = true
			break
		}
	}

	if !hasGitOpsAction {
		return nil, nil
	}

	ci, err := query.GetCachedConfig(ctx, run.ConfigID.String())
	if err != nil {
		return nil, err
	}
	_ = ci

	var gitOpsEnv GitOpsEnv

	gitRepos := query.TraverseConfig(ctx, run.ConfigID.String(), "Kubernetes::Kustomization/Kubernetes::GitRepository", string(models.RelatedConfigTypeIncoming))
	if len(gitRepos) > 0 && gitRepos[0].Config != nil {
		var config map[string]any
		if err := json.Unmarshal([]byte(*gitRepos[0].Config), &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}

		gitOpsEnv.Git.URL, _, _ = unstructured.NestedString(config, "spec", "url")
		gitOpsEnv.Git.Branch, _, _ = unstructured.NestedString(config, "spec", "ref", "branch")
		_origin, _, _ := unstructured.NestedString(config, "metadata", "annotations", "config.kubernetes.io/origin")
		if _origin != "" {
			var origin map[string]any
			if err := yaml.Unmarshal([]byte(_origin), &origin); err != nil {
				ctx.Tracef("failed to unmarshal origin: %v", err)
			} else if path, ok := origin["path"]; ok {
				gitOpsEnv.Git.File = path.(string)
			}
		}
	}

	kustomization := query.TraverseConfig(ctx, run.ConfigID.String(), "Kubernetes::Kustomization", string(models.RelatedConfigTypeIncoming))
	if len(kustomization) > 0 && kustomization[0].Config != nil {
		var config map[string]any
		if err := json.Unmarshal([]byte(*kustomization[0].Config), &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}

		gitOpsEnv.Kustomize.Path, _, _ = unstructured.NestedString(config, "spec", "path")
	}

	return &gitOpsEnv, nil
}
