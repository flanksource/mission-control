package tools

import (
	"context"
	"encoding/json"
	"fmt"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

const CatalogToolName = "getCatalogByNameOrID"

func NewCatalogTool(ctx dutyContext.Context) *CatalogTool {
	return &CatalogTool{dutyCtx: ctx}
}

type CatalogTool struct {
	dutyCtx dutyContext.Context
}

func (t *CatalogTool) Name() string {
	return CatalogToolName
}

func (t *CatalogTool) Description() string {
	return `Retrieves Kubernetes object manifest by UID or namespace/name pair.

Input format: {"uid": "<kubernetes-object-uid>"}
Output: JSON manifest of the Kubernetes object

Example:
Input: {"uid": "1234-5678-90ab-cdef"}
Input: {"namespace": "default", "name": "alertmanager-0"}
Output: {"apiVersion": "v1", "kind": "Pod", ...}

This tool can be used again and again specially to traverse a config.
Example: When the user asks to find the deployment of a pod, you can
use this tool to get the replicaset and then the deployment.
`
}

func (t *CatalogTool) Call(ctx context.Context, input string) (string, error) {
	var data CatalogRequest
	if err := json.Unmarshal([]byte(input), &data); err != nil {
		return "", err
	}

	var config models.ConfigItem
	if _, err := uuid.Parse(data.ID); err == nil {
		if c, err := query.GetCachedConfig(t.dutyCtx, data.ID); err != nil {
			return "", err
		} else if c == nil {
			return "config was not found", nil
		} else {
			config = *c
		}
	} else if data.Name != "" {
		// TODO: add scraper id and cluster id
		if err := t.dutyCtx.DB().Where("tags->>'namespace' = ?", data.Namespace).Where("name = ?", data.Name).Find(&config).Error; err != nil {
			return "", err
		} else if config.ID == uuid.Nil {
			return "config was not found", nil
		}
	}

	manifest := lo.FromPtr(config.Config)
	fmt.Println(manifest)

	return manifest, nil
}

type CatalogRequest struct {
	ID        string `json:"uid"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}
