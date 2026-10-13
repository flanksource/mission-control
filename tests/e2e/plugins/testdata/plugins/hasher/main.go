package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

type hasherPlugin struct{}

type hashResult struct {
	ConfigID string `json:"config_id"`
	SHA256   string `json:"sha256"`
}

func (hasherPlugin) Manifest() *sdk.PluginManifest {
	return &sdk.PluginManifest{
		Name:        "hasher",
		Version:     "1.0.0",
		Description: "E2E test plugin that hashes config ids",
	}
}

func (hasherPlugin) Configure(context.Context, map[string]any) error { return nil }

func (hasherPlugin) Operations() []sdk.Operation {
	def := &sdk.OperationDef{
		Name:        "sha256",
		Description: "returns the sha256 of the config id",
		ResultMime:  "application/json",
		Http:        []*sdk.HTTPBinding{{Method: http.MethodGet}},
	}

	return []sdk.Operation{{
		Def: def,
		Handler: func(ctx context.Context, req sdk.InvokeCtx) (any, error) {
			return hashConfigID(req.ConfigItemID), nil
		},
		HTTPHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := hashConfigID(sdk.ConfigItemIDFromContext(r.Context()))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		}),
	}}
}

// hashConfigID needs no host callback: the plugin is granted nothing, so it can't read the config it's invoked on.
func hashConfigID(configID string) hashResult {
	sum := sha256.Sum256([]byte(configID))
	return hashResult{ConfigID: configID, SHA256: hex.EncodeToString(sum[:])}
}

func main() {
	sdk.Serve(&hasherPlugin{})
}
