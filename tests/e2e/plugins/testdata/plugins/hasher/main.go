package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

type hasherPlugin struct{}

type hashResult struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func (hasherPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:        "hasher",
		Version:     "1.0.0",
		Description: "E2E test plugin that hashes config names",
	}
}

func (hasherPlugin) Configure(context.Context, map[string]any) error { return nil }

func (hasherPlugin) Operations() []sdk.Operation {
	def := &pluginpb.OperationDef{
		Name:        "sha256",
		Description: "returns the sha256 of the config name",
		ResultMime:  "application/json",
		Http:        []*pluginpb.HTTPBinding{{Method: http.MethodGet}},
	}

	return []sdk.Operation{{
		Def: def,
		Handler: func(ctx context.Context, req sdk.InvokeCtx) (any, error) {
			return hashConfigName(ctx, req.Host, req.ConfigItemID)
		},
		HTTPHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := hashConfigName(r.Context(), sdk.HostClientFromContext(r.Context()), sdk.ConfigItemIDFromContext(r.Context()))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		}),
	}}
}

func hashConfigName(ctx context.Context, host sdk.HostClient, configID string) (hashResult, error) {
	if host == nil {
		return hashResult{}, fmt.Errorf("host client is nil")
	}
	item, err := host.GetConfigItem(ctx, configID)
	if err != nil {
		return hashResult{}, err
	}
	sum := sha256.Sum256([]byte(item.Name))
	return hashResult{Name: item.Name, SHA256: hex.EncodeToString(sum[:])}, nil
}

func main() {
	sdk.Serve(&hasherPlugin{})
}
