package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/plugin/sdk"
	golangk8s "github.com/flanksource/incident-commander/plugins/golang/internal/k8s"
)

type SessionCreateParams struct {
	Namespace     string `json:"namespace,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Name          string `json:"name,omitempty"`
	Pod           string `json:"pod,omitempty"`
	Container     string `json:"container,omitempty"`
	PID           int    `json:"pid,omitempty"`
	GopsPort      int    `json:"gopsPort,omitempty"`
	PprofPort     int    `json:"pprofPort,omitempty"`
	PprofBasePath string `json:"pprofBasePath,omitempty"`
	GopsConfigDir string `json:"gopsConfigDir,omitempty"`
	LocalGops     int    `json:"localGops,omitempty"`
	LocalPprof    int    `json:"localPprof,omitempty"`
}

type SessionDeleteParams struct {
	ID string `json:"id"`
}

type SessionIDParams struct {
	SessionID string `json:"sessionId"`
}

type ProfileCollectParams struct {
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Seconds   int    `json:"seconds,omitempty"`
	Source    string `json:"source,omitempty"`
}

type ProfileRunParams struct {
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Seconds   int    `json:"seconds,omitempty"`
	Source    string `json:"source,omitempty"`
}

type ProfileRunIDParams struct {
	SessionID string `json:"sessionId,omitempty"`
	RunID     string `json:"runId"`
}

type RuntimeSnapshot struct {
	SessionID string `json:"sessionId"`
	Version   string `json:"version,omitempty"`
	Stats     string `json:"stats,omitempty"`
	MemStats  string `json:"memstats,omitempty"`
	Error     string `json:"error,omitempty"`
}

type GoroutineSnapshot struct {
	SessionID string `json:"sessionId"`
	Source    string `json:"source"`
	Dump      string `json:"dump"`
}

type ProfileResult struct {
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId,omitempty"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	Bytes     int    `json:"bytes"`
	URL       string `json:"url"`
	Seconds   int    `json:"seconds,omitempty"`
}

type portCandidate struct {
	Remote int
	Local  int
	Source string
}

func (p *GolangPlugin) podsList(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	target, err := targetFromConfig(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	cli, err := p.clients.Client(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	return listRunningPodsForTarget(ctx, cli, target)
}

func (p *GolangPlugin) sessionsList(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	return p.sessions.List(), nil
}

func (p *GolangPlugin) sessionCreate(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionCreateParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
	}
	if p.sessions.RunningCount() >= p.settings.MaxSessions {
		return nil, fmt.Errorf("maximum running sessions reached (%d)", p.settings.MaxSessions)
	}
	target, err := p.createTarget(ctx, req, params)
	if err != nil {
		return nil, err
	}
	cli, err := p.clients.Client(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	restCfg, err := p.clients.RESTConfig(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	pods, err := listRunningPodsForTarget(ctx, cli, target)
	if err != nil {
		return nil, fmt.Errorf("resolve pods: %w", err)
	}
	pod, container, err := selectPodContainer(pods, params.Pod, params.Container)
	if err != nil {
		return nil, err
	}

	var diagnostics []string
	gopsPort := params.GopsPort
	pid := params.PID
	dirs := append([]string{}, p.settings.GopsConfigDirs...)
	if params.GopsConfigDir != "" {
		dirs = append([]string{params.GopsConfigDir}, dirs...)
	}
	if gopsPort == 0 {
		discoverCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		procs, err := discoverGopsProcesses(discoverCtx, restCfg, pod.Namespace, pod.Name, container, dirs)
		if err != nil {
			diagnostics = append(diagnostics, err.Error())
		} else if proc, ok := selectGopsProcess(procs, pid); ok {
			gopsPort = proc.Port
			pid = proc.PID
			diagnostics = append(diagnostics, fmt.Sprintf("discovered gops pid=%d port=%d", proc.PID, proc.Port))
		} else if params.PID > 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("no gops port file found for pid %d", params.PID))
		} else {
			diagnostics = append(diagnostics, "no gops port file found")
		}
	}
	gopsPorts := gopsCandidatePorts(gopsPort, p.settings.DefaultGopsPort, p.settings.DefaultGopsPorts)
	if gopsPort == 0 && len(gopsPorts) > 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("trying default gops ports: %s", formatPorts(gopsPorts)))
	}

	pprofPort := params.PprofPort
	pprofBase := p.settings.PprofBasePath
	if params.PprofBasePath != "" {
		pprofBase = normalizePprofBase(params.PprofBasePath)
	}
	pprofPorts := pprofCandidatePorts(pprofPort, p.settings.DefaultPprofPort, pod.ContainerPorts[container])
	if pprofPort == 0 && len(pod.ContainerPorts[container]) > 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("trying %s on declared container ports: %s", pprofBase, formatPorts(pod.ContainerPorts[container])))
	}

	var mappings []golangk8s.PortMapping
	var gopsCandidates []portCandidate
	for i, remote := range gopsPorts {
		preferred := 0
		if i == 0 {
			preferred = params.LocalGops
		}
		local, err := pickLocalPort(preferred)
		if err != nil {
			return nil, err
		}
		gopsCandidates = append(gopsCandidates, portCandidate{Remote: remote, Local: local, Source: "gops"})
		mappings = append(mappings, golangk8s.PortMapping{LocalPort: local, RemotePort: remote})
	}
	var pprofCandidates []portCandidate
	for i, remote := range pprofPorts {
		preferred := 0
		if i == 0 {
			preferred = params.LocalPprof
		}
		local, err := pickLocalPort(preferred)
		if err != nil {
			return nil, err
		}
		pprofCandidates = append(pprofCandidates, portCandidate{Remote: remote, Local: local, Source: "pprof"})
		mappings = append(mappings, golangk8s.PortMapping{LocalPort: local, RemotePort: remote})
	}
	if len(mappings) == 0 {
		return nil, fmt.Errorf("no diagnostics endpoint candidates found; configure gopsPort, pprofPort, default plugin ports, a readable gopsConfigDir, or containerPorts")
	}

	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fwd, ready, err := golangk8s.StartPortForward(restCfg, pod.Namespace, pod.Name, mappings, io.Discard, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("port-forward: %w", err)
	}
	if err := fwd.Ready(startCtx, ready); err != nil {
		_ = fwd.Close()
		return nil, fmt.Errorf("port-forward not ready: %w", err)
	}
	sess := NewSession(pod.Namespace, target.Kind, target.Name, pod.Name, container, func() error { return fwd.Close() })
	sess.PID = pid
	if match, ok := firstWorkingGops(ctx, gopsCandidates); ok {
		sess.GopsRemote = match.Remote
		sess.GopsLocal = match.Local
		sess.GopsAvailable = true
	}
	if match, ok := firstWorkingPprof(ctx, pprofCandidates, pprofBase); ok {
		sess.PprofRemote = match.Remote
		sess.PprofLocal = match.Local
		sess.PprofAvailable = true
	}
	sess.PprofBasePath = pprofBase
	sess.Diagnostics = diagnostics
	if len(gopsCandidates) > 0 && !sess.GopsAvailable {
		sess.Diagnostics = append(sess.Diagnostics, fmt.Sprintf("no gops agent responded on candidate ports: %s", formatCandidatePorts(gopsCandidates)))
	}
	if len(pprofCandidates) > 0 && !sess.PprofAvailable {
		sess.Diagnostics = append(sess.Diagnostics, fmt.Sprintf("no pprof index responded at %s on candidate ports: %s", pprofBase, formatCandidatePorts(pprofCandidates)))
	}
	if !sess.GopsAvailable && !sess.PprofAvailable {
		_ = fwd.Close()
		return nil, fmt.Errorf("no diagnostics endpoint responded; %s", strings.Join(sess.Diagnostics, "; "))
	}
	p.sessions.Add(sess)
	return sess.Snapshot(), nil
}

func (p *GolangPlugin) createTarget(ctx context.Context, req sdk.InvokeCtx, params SessionCreateParams) (TargetRef, error) {
	if params.Pod != "" {
		ns := params.Namespace
		if ns == "" {
			base, err := targetFromConfig(ctx, req.Host, req.ConfigItemID)
			if err != nil {
				return TargetRef{}, err
			}
			ns = base.Namespace
		}
		return TargetRef{Namespace: ns, Kind: "pod", Name: params.Pod}, nil
	}
	if params.Kind != "" && params.Name != "" && params.Namespace != "" {
		return TargetRef{Namespace: params.Namespace, Kind: normalizeKind(params.Kind), Name: params.Name}, nil
	}
	base, err := targetFromConfig(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return TargetRef{}, err
	}
	if params.Kind != "" && params.Name != "" {
		base.Kind = normalizeKind(params.Kind)
		base.Name = params.Name
	}
	if params.Namespace != "" {
		base.Namespace = params.Namespace
	}
	return base, nil
}

func (p *GolangPlugin) sessionDelete(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionDeleteParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	removed, err := p.sessions.Remove(params.ID)
	if !removed {
		return nil, fmt.Errorf("session %q not found", params.ID)
	}
	p.profiles.RemoveSession(params.ID)
	if p.viewers != nil {
		p.viewers.RemoveSession(params.ID)
	}
	return map[string]any{"deleted": true, "id": params.ID}, err
}

func (p *GolangPlugin) runtimeSnapshot(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	sess, err := p.sessionFromRequest(req.ParamsJSON)
	if err != nil {
		return nil, err
	}
	if !sess.GopsAvailable {
		return nil, fmt.Errorf("session %s has no reachable gops agent", sess.ID)
	}
	client := GopsClient{Addr: gopsAddr(sess), Timeout: 10 * time.Second}
	out := RuntimeSnapshot{SessionID: sess.ID}
	out.Version, _ = client.Version(ctx)
	out.Stats, _ = client.Stats(ctx)
	out.MemStats, _ = client.MemStats(ctx)
	if out.Version == "" && out.Stats == "" && out.MemStats == "" {
		out.Error = "gops agent returned no runtime data"
	}
	return out, nil
}

func (p *GolangPlugin) goroutines(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	sess, err := p.sessionFromRequest(req.ParamsJSON)
	if err != nil {
		return nil, err
	}
	if sess.GopsAvailable {
		dump, err := (GopsClient{Addr: gopsAddr(sess), Timeout: 15 * time.Second}).Stack(ctx)
		if err != nil {
			return nil, err
		}
		return GoroutineSnapshot{SessionID: sess.ID, Source: "gops", Dump: dump}, nil
	}
	if sess.PprofAvailable {
		body, err := getPprof(ctx, sess, "goroutine?debug=2")
		if err != nil {
			return nil, err
		}
		return GoroutineSnapshot{SessionID: sess.ID, Source: "pprof", Dump: string(body)}, nil
	}
	return nil, fmt.Errorf("session %s has neither gops nor pprof available", sess.ID)
}

func (p *GolangPlugin) profileCollect(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params ProfileCollectParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	sess, ok := p.sessions.Get(params.SessionID)
	if !ok {
		return nil, fmt.Errorf("session %q not found", params.SessionID)
	}
	kind := normalizeProfileKind(params.Kind)
	if kind == "" {
		return nil, fmt.Errorf("profile kind must be heap, cpu, or trace")
	}
	preference := normalizeProfileSource(params.Source)
	seconds := params.Seconds
	if seconds <= 0 || seconds > p.settings.MaxProfileSec {
		seconds = p.settings.MaxProfileSec
	}
	data, source, err := collectProfileWithSource(ctx, sess, kind, seconds, preference)
	if err != nil {
		return nil, err
	}
	run, _ := NewProfileRun(sess.ID, kind, preference, seconds)
	run.MarkDone(data, source, nil)
	p.profiles.Add(run)
	return ProfileResult{
		SessionID: sess.ID,
		RunID:     run.ID,
		Kind:      kind,
		Source:    source,
		Bytes:     len(data),
		URL:       fmt.Sprintf("profiles/%s/%s", sess.ID, run.ID),
		Seconds:   seconds,
	}, nil
}

func (p *GolangPlugin) profileStart(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params ProfileRunParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	sess, ok := p.sessions.Get(params.SessionID)
	if !ok {
		return nil, fmt.Errorf("session %q not found", params.SessionID)
	}
	kind := normalizeProfileKind(params.Kind)
	if kind == "" {
		return nil, fmt.Errorf("profile kind must be heap, cpu, or trace")
	}
	preference := normalizeProfileSource(params.Source)
	seconds := params.Seconds
	if seconds <= 0 || seconds > p.settings.MaxProfileSec {
		seconds = p.settings.MaxProfileSec
	}
	run, runCtx := NewProfileRun(sess.ID, kind, preference, seconds)
	p.profiles.Add(run)
	go func() {
		timeout := time.Duration(seconds+15) * time.Second
		if timeout < 45*time.Second {
			timeout = 45 * time.Second
		}
		ctx, cancel := context.WithTimeout(runCtx, timeout)
		defer cancel()
		data, source, err := collectProfileWithSource(ctx, sess, kind, seconds, preference)
		run.MarkDone(data, source, err)
	}()
	return run.Snapshot(), nil
}

func (p *GolangPlugin) profileStatus(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params ProfileRunIDParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.RunID == "" {
		return nil, fmt.Errorf("runId is required")
	}
	run, ok := p.profiles.Get(params.RunID)
	if !ok {
		return nil, fmt.Errorf("profile run %q not found", params.RunID)
	}
	if params.SessionID != "" && run.SessionID != params.SessionID {
		return nil, fmt.Errorf("profile run %q does not belong to session %q", params.RunID, params.SessionID)
	}
	return run.Snapshot(), nil
}

func (p *GolangPlugin) profileStop(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params ProfileRunIDParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.RunID == "" {
		return nil, fmt.Errorf("runId is required")
	}
	run, ok := p.profiles.Get(params.RunID)
	if !ok {
		return nil, fmt.Errorf("profile run %q not found", params.RunID)
	}
	if params.SessionID != "" && run.SessionID != params.SessionID {
		return nil, fmt.Errorf("profile run %q does not belong to session %q", params.RunID, params.SessionID)
	}
	run.Stop()
	return run.Snapshot(), nil
}

func (p *GolangPlugin) profileRunsList(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionIDParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("sessionId is required")
	}
	if _, ok := p.sessions.Get(params.SessionID); !ok {
		return nil, fmt.Errorf("session %q not found", params.SessionID)
	}
	return p.profiles.List(params.SessionID), nil
}

func (p *GolangPlugin) sessionFromRequest(raw []byte) (*Session, error) {
	var params SessionIDParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("sessionId is required")
	}
	sess, ok := p.sessions.Get(params.SessionID)
	if !ok {
		return nil, fmt.Errorf("session %q not found", params.SessionID)
	}
	return sess, nil
}

func selectPodContainer(pods []RunningPod, podName, container string) (RunningPod, string, error) {
	if len(pods) == 0 {
		return RunningPod{}, "", fmt.Errorf("no ready pods found")
	}
	for _, pod := range pods {
		if podName != "" && pod.Name != podName {
			continue
		}
		if container == "" && len(pod.Containers) == 1 {
			container = pod.Containers[0]
		}
		if container == "" {
			return pod, "", fmt.Errorf("container is required for pod %s because it has %d containers", pod.Name, len(pod.Containers))
		}
		for _, c := range pod.Containers {
			if c == container {
				return pod, container, nil
			}
		}
		return pod, "", fmt.Errorf("container %q not found in pod %s", container, pod.Name)
	}
	return RunningPod{}, "", fmt.Errorf("pod %q not found in ready target pods", podName)
}

func probeGops(ctx context.Context, port int) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := (GopsClient{Addr: fmt.Sprintf("127.0.0.1:%d", port), Timeout: 3 * time.Second}).Version(probeCtx)
	return err == nil
}

func probePprof(ctx context.Context, port int, base string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s/", port, normalizePprofBase(base)), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func firstWorkingGops(ctx context.Context, candidates []portCandidate) (portCandidate, bool) {
	for _, candidate := range candidates {
		if probeGops(ctx, candidate.Local) {
			return candidate, true
		}
	}
	return portCandidate{}, false
}

func firstWorkingPprof(ctx context.Context, candidates []portCandidate, base string) (portCandidate, bool) {
	for _, candidate := range candidates {
		if probePprof(ctx, candidate.Local, base) {
			return candidate, true
		}
	}
	return portCandidate{}, false
}

func collectProfile(ctx context.Context, sess *Session, kind string, seconds int) ([]byte, string, error) {
	return collectProfileWithSource(ctx, sess, kind, seconds, "auto")
}

func collectProfileWithSource(ctx context.Context, sess *Session, kind string, seconds int, preference string) ([]byte, string, error) {
	if preference == "" {
		preference = "auto"
	}
	if preference == "pprof" {
		if !sess.PprofAvailable {
			return nil, "", fmt.Errorf("pprof is not available for session %s", sess.ID)
		}
		return collectPprofProfile(ctx, sess, kind, seconds)
	}
	if preference == "gops" {
		if !sess.GopsAvailable {
			return nil, "", fmt.Errorf("gops is not available for session %s", sess.ID)
		}
		return collectGopsProfile(ctx, sess, kind)
	}
	if sess.PprofAvailable {
		data, source, err := collectPprofProfile(ctx, sess, kind, seconds)
		if err == nil {
			return data, source, nil
		}
		if !sess.GopsAvailable {
			return nil, "", err
		}
	}
	if !sess.GopsAvailable {
		return nil, "", fmt.Errorf("session %s has neither pprof nor gops available", sess.ID)
	}
	return collectGopsProfile(ctx, sess, kind)
}

func collectPprofProfile(ctx context.Context, sess *Session, kind string, seconds int) ([]byte, string, error) {
	path := kind
	if kind == "cpu" {
		path = fmt.Sprintf("profile?seconds=%d", seconds)
	}
	if kind == "trace" {
		path = fmt.Sprintf("trace?seconds=%d", seconds)
	}
	data, err := getPprof(ctx, sess, path)
	return data, "pprof", err
}

func collectGopsProfile(ctx context.Context, sess *Session, kind string) ([]byte, string, error) {
	client := GopsClient{Addr: gopsAddr(sess), Timeout: 45 * time.Second}
	switch kind {
	case "heap":
		data, err := client.Heap(ctx)
		return data, "gops", err
	case "cpu":
		data, err := client.CPU(ctx)
		return data, "gops", err
	case "trace":
		data, err := client.Trace(ctx)
		return data, "gops", err
	default:
		return nil, "", fmt.Errorf("unsupported profile kind %q", kind)
	}
}

func getPprof(ctx context.Context, sess *Session, path string) ([]byte, error) {
	if !sess.PprofAvailable || sess.PprofLocal == 0 {
		return nil, fmt.Errorf("pprof is not available for session %s", sess.ID)
	}
	base := strings.TrimRight(normalizePprofBase(sess.PprofBasePath), "/")
	path = strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s/%s", sess.PprofLocal, base, path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pprof %s returned HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func normalizePprofBase(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/debug/pprof"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(path, "/")
}

func normalizeProfileKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "heap", "pprof-heap":
		return "heap"
	case "cpu", "profile", "pprof-cpu":
		return "cpu"
	case "trace":
		return "trace"
	default:
		return ""
	}
}

func normalizeProfileSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "auto":
		return "auto"
	case "pprof":
		return "pprof"
	case "gops":
		return "gops"
	default:
		return "auto"
	}
}

func gopsCandidatePorts(discovered, configured int, defaults []int) []int {
	if discovered > 0 {
		return []int{discovered}
	}
	out := []int{}
	if configured > 0 {
		out = append(out, configured)
	}
	out = append(out, defaults...)
	return uniquePositiveInts(out...)
}

func pprofCandidatePorts(explicit, configured int, containerPorts []int) []int {
	if explicit > 0 {
		return []int{explicit}
	}
	out := []int{}
	if configured > 0 {
		out = append(out, configured)
	}
	out = append(out, containerPorts...)
	return uniquePositiveInts(out...)
}

func uniquePositiveInts(values ...int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func formatPorts(ports []int) string {
	parts := make([]string, 0, len(ports))
	for _, port := range uniquePositiveInts(ports...) {
		parts = append(parts, fmt.Sprint(port))
	}
	return strings.Join(parts, ", ")
}

func formatCandidatePorts(candidates []portCandidate) string {
	ports := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		ports = append(ports, candidate.Remote)
	}
	return formatPorts(ports)
}
