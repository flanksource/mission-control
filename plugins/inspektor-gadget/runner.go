package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/client-go/rest"
)

type TraceRunRequest struct {
	Image           string
	Params          map[string]string
	RESTConfig      *rest.Config
	GadgetNamespace string
	Timeout         time.Duration
}

type TraceRunner interface {
	Run(ctx context.Context, req TraceRunRequest, emit func(TraceEvent)) error
}

func decodeEvent(raw []byte) TraceEvent {
	event := TraceEvent{Raw: string(raw)}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err == nil {
		event.Data = data
		if k8s, ok := data["k8s"].(map[string]any); ok {
			if node, ok := k8s["node"].(string); ok {
				event.Node = node
			}
		}
	}
	return event
}

func validateRunRequest(req TraceRunRequest) error {
	if req.RESTConfig == nil {
		return fmt.Errorf("kubernetes rest config is required")
	}
	if req.Image == "" {
		return fmt.Errorf("gadget image is required")
	}
	return nil
}
