package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

type TraceStartParams struct {
	Gadget      string         `json:"gadget"`
	Namespace   string         `json:"namespace,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	Name        string         `json:"name,omitempty"`
	Pod         string         `json:"pod,omitempty"`
	Container   string         `json:"container,omitempty"`
	DurationSec int            `json:"durationSec,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Args        []string       `json:"args,omitempty"`
	ArgString   string         `json:"argString,omitempty"`
}

type TraceStopParams struct {
	ID string `json:"id"`
}

type TraceEventsParams struct {
	ID string `json:"id"`
}

func (p *InspektorGadgetPlugin) tracesList(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	return supportedGadgets(p.settings.GadgetTag), nil
}

func (p *InspektorGadgetPlugin) traceList(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	return p.sessions.List(), nil
}

func (p *InspektorGadgetPlugin) traceEvents(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params TraceEventsParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	sess, ok := p.sessions.Get(params.ID)
	if !ok {
		return nil, fmt.Errorf("session %q not found", params.ID)
	}
	return sess.Events(), nil
}

func (p *InspektorGadgetPlugin) traceStop(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params TraceStopParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	sess, ok := p.sessions.Get(params.ID)
	if !ok {
		return nil, fmt.Errorf("session %q not found", params.ID)
	}
	sess.Stop()
	return sess.Snapshot(), nil
}

func (p *InspektorGadgetPlugin) traceStart(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params TraceStartParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
	}
	if params.Gadget == "" {
		params.Gadget = "trace_exec"
	}
	if p.sessions.RunningCount() >= p.settings.MaxSessions {
		return nil, fmt.Errorf("maximum running sessions reached (%d)", p.settings.MaxSessions)
	}
	gadget, ok := gadgetByID(params.Gadget, p.settings.GadgetTag)
	if !ok {
		return nil, fmt.Errorf("unsupported gadget %q", params.Gadget)
	}

	cli, err := p.clients.Client(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	restCfg, err := p.clients.RESTConfig(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	target, err := p.createTraceTarget(ctx, req, params)
	if err != nil {
		return nil, err
	}
	pods, err := listRunningPodsForTarget(ctx, cli, TargetRef{Namespace: target.Namespace, Kind: target.Kind, Name: target.Name, Selector: target.Selector})
	if err != nil {
		return nil, fmt.Errorf("resolve pods: %w", err)
	}
	if target.Pod == "" {
		if len(pods) == 0 {
			return nil, fmt.Errorf("no ready pods found for %s/%s in namespace %s", target.Kind, target.Name, target.Namespace)
		}
		target.Pod = pods[0].Name
		target.Node = pods[0].Node
		if target.Container == "" && len(pods[0].Containers) == 1 {
			target.Container = pods[0].Containers[0]
		}
	} else if len(pods) > 0 {
		target.Node = pods[0].Node
		if target.Container == "" && len(pods[0].Containers) == 1 {
			target.Container = pods[0].Containers[0]
		}
	}
	if target.Selector == nil && target.Pod == "" && len(pods) > 0 {
		target.Selector = pods[0].Labels
	}

	options, err := normalizeTraceOptions(params)
	if err != nil {
		return nil, err
	}
	runParams := buildGadgetParams(target, options)
	duration := p.duration(params.DurationSec)
	diagnostics := TraceDiagnostics{
		Runtime:      "inspektor-gadget-gadget-service-grpc",
		Connection:   "kubernetes-api-portforward",
		DurationSec:  int(duration.Seconds()),
		MaxSessions:  p.settings.MaxSessions,
		ResolvedPods: pods,
		UserOptions:  options,
	}
	if req.Caller != nil {
		diagnostics.StartedByUserID = req.Caller.UserId
		diagnostics.StartedByEmail = req.Caller.UserEmail
	}
	session, runCtx := newTraceSession(gadget, target, runParams, diagnostics, p.settings.MaxEvents)
	p.sessions.Add(session)

	go func() {
		ctx, cancel := context.WithTimeout(runCtx, duration)
		defer cancel()
		session.MarkRunning()
		err := p.runner.Run(ctx, TraceRunRequest{
			Image:           gadget.Image,
			Params:          runParams,
			RESTConfig:      restCfg,
			GadgetNamespace: p.settings.GadgetNamespace,
			Timeout:         duration,
		}, session.AddEvent)
		session.MarkDone(err)
	}()

	return session.Snapshot(), nil
}

func normalizeTraceOptions(params TraceStartParams) (map[string]any, error) {
	options := map[string]any{}
	for k, v := range params.Options {
		if strings.TrimSpace(k) != "" && v != nil {
			options[strings.TrimSpace(k)] = v
		}
	}
	for k, v := range params.Arguments {
		if strings.TrimSpace(k) != "" && v != nil {
			options[strings.TrimSpace(k)] = v
		}
	}
	if params.ArgString != "" {
		parsed, err := parseArgLines(strings.Split(params.ArgString, "\n"))
		if err != nil {
			return nil, err
		}
		for k, v := range parsed {
			options[k] = v
		}
	}
	parsed, err := parseArgLines(params.Args)
	if err != nil {
		return nil, err
	}
	for k, v := range parsed {
		options[k] = v
	}
	return options, nil
}

func parseArgLines(lines []string) (map[string]any, error) {
	out := map[string]any{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "--")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			key, value, ok = strings.Cut(line, ":")
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid gadget argument %q", line)
		}
		if !ok {
			out[key] = true
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out, nil
}

func (p *InspektorGadgetPlugin) createTraceTarget(ctx context.Context, req sdk.InvokeCtx, params TraceStartParams) (TraceTarget, error) {
	if params.Pod != "" {
		ns := params.Namespace
		base := TargetRef{}
		if ns == "" {
			var err error
			base, err = targetFromConfig(ctx, req.Host, req.ConfigItemID)
			if err != nil {
				return TraceTarget{}, err
			}
			ns = base.Namespace
		}
		return TraceTarget{Namespace: ns, Kind: "pod", Name: params.Pod, Pod: params.Pod, Container: params.Container}, nil
	}
	if params.Kind != "" && params.Name != "" && params.Namespace != "" {
		return TraceTarget{Namespace: params.Namespace, Kind: normalizeKind(params.Kind), Name: params.Name, Container: params.Container}, nil
	}
	base, err := targetFromConfig(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return TraceTarget{}, err
	}
	if params.Kind != "" && params.Name != "" {
		base.Kind = normalizeKind(params.Kind)
		base.Name = params.Name
	}
	if params.Namespace != "" {
		base.Namespace = params.Namespace
	}
	return TraceTarget{Namespace: base.Namespace, Kind: base.Kind, Name: base.Name, Container: params.Container}, nil
}

func (p *InspektorGadgetPlugin) duration(requested int) time.Duration {
	max := p.settings.MaxDurationSec
	if max <= 0 {
		max = 300
	}
	if requested <= 0 || requested > max {
		requested = max
	}
	return time.Duration(requested) * time.Second
}
