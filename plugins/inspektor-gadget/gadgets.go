package main

import (
	"fmt"
	"sort"
	"strings"
)

type GadgetSpec struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Image       string       `json:"image"`
	Description string       `json:"description"`
	Kind        string       `json:"kind"`
	Widget      string       `json:"widget"`
	Category    string       `json:"category"`
	Icon        string       `json:"icon"`
	DocsURL     string       `json:"docsUrl"`
	Streaming   bool         `json:"streaming"`
	Options     []Option     `json:"options,omitempty"`
	EventSchema *EventSchema `json:"eventSchema,omitempty"`
}

type Option struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
}

func supportedGadgets(tag string) []GadgetSpec {
	if tag == "" {
		tag = defaultIGTag
	}
	image := func(id string) string {
		return fmt.Sprintf("ghcr.io/inspektor-gadget/gadget/%s:%s", id, tag)
	}
	mk := func(id, name, desc, kind, category, icon string, streaming bool, options ...Option) GadgetSpec {
		return GadgetSpec{
			ID:          id,
			Name:        name,
			Image:       image(id),
			Description: desc,
			Kind:        kind,
			Widget:      widgetForKind(kind),
			Category:    category,
			Icon:        icon,
			DocsURL:     "https://inspektor-gadget.io/docs/latest/gadgets/" + id + "/",
			Streaming:   streaming,
			Options:     options,
		}
	}
	out := []GadgetSpec{
		mk("advise_networkpolicy", "Advise NetworkPolicy", "Monitor network activity and generate Kubernetes NetworkPolicy guidance.", "advise", "Advise", "shield", true),
		mk("advise_seccomp", "Advise Seccomp", "Record syscalls and generate a seccomp profile for the selected workload.", "advise", "Advise", "shield", true),
		mk("audit_seccomp", "Audit Seccomp", "Stream syscalls that were audited by seccomp.", "audit", "Security", "shield-alert", true),
		mk("bpfstats", "BPF Stats", "Report CPU and memory usage for Gadgets and eBPF programs.", "top", "Observability", "activity", true),
		mk("deadlock", "Deadlock", "Trace pthread mutex lock/unlock calls and detect potential deadlocks.", "trace", "Runtime", "lock", true),
		mk("fdpass", "FD Passing", "Trace file descriptor passing over unix sockets.", "trace", "Files", "send", true),
		mk("fsnotify", "FS Notify", "Detect applications using inotify or fanotify.", "trace", "Files", "folder-search", true),
		mk("profile_blockio", "Block I/O Profile", "Gather block I/O latency profile data.", "profile", "Profile", "hard-drive", false),
		mk("profile_cpu", "CPU Profile", "Sample stack traces for CPU profiling.", "profile", "Profile", "cpu", false),
		mk("profile_cuda", "CUDA Profile", "Profile CUDA memory allocations in libcuda.so.", "profile", "Profile", "gpu", false),
		mk("profile_qdisc_latency", "Qdisc Latency Profile", "Gather queuing discipline latency profile data.", "profile", "Profile", "timer", false),
		mk("profile_tcprtt", "TCP RTT Profile", "Generate a TCP round-trip-time histogram.", "profile", "Network", "timer", false),
		mk("snapshot_file", "File Snapshot", "Show open files for running processes.", "snapshot", "Files", "file-search", false),
		mk("snapshot_process", "Process Snapshot", "Show running processes.", "snapshot", "Runtime", "list-tree", false),
		mk("snapshot_socket", "Socket Snapshot", "Show existing sockets.", "snapshot", "Network", "plug", false),
		mk("tcpdump", "TCP Dump", "Capture packets in container contexts with pcap-compatible filters.", "trace", "Network", "traffic-cone", true),
		mk("top_blockio", "Block I/O Top", "Periodically report block device I/O activity.", "top", "Observability", "hard-drive", true),
		mk("top_cpu_throttle", "CPU Throttle Top", "Periodically report cgroup v2 CPU throttling.", "top", "Observability", "gauge", true),
		mk("top_file", "File Top", "Periodically report file read/write activity.", "top", "Files", "file-chart-column", true),
		mk("top_process", "Process Top", "Periodically report process CPU, memory, and runtime statistics.", "top", "Runtime", "monitor-cog", true),
		mk("top_tcp", "TCP Top", "Periodically report TCP send/receive activity by connection.", "top", "Network", "network", true),
		mk("trace_bind", "Bind", "Stream socket bind syscalls.", "trace", "Network", "plug-zap", true),
		mk("trace_capabilities", "Capabilities", "Trace Linux capability security checks.", "trace", "Security", "key-round", true),
		mk("trace_dns", "DNS", "Trace DNS queries and responses.", "trace", "Network", "globe", true),
		mk("trace_exec", "Exec", "Trace new process executions.", "trace", "Runtime", "terminal-square", true,
			Option{Name: "ignore-failed", Type: "boolean", Description: "Ignore failed exec attempts.", Default: true},
			Option{Name: "paths", Type: "boolean", Description: "Include current working directory and executable path.", Default: false},
		),
		mk("trace_fsslower", "Filesystem Slower", "Trace slow open, read, write, and fsync file operations.", "trace", "Files", "file-clock", true),
		mk("trace_init_module", "Kernel Module Load", "Trace init_module and finit_module syscalls.", "trace", "Security", "package-plus", true),
		mk("trace_lsm", "LSM", "Trace Linux Security Module hook activity.", "trace", "Security", "shield-check", true),
		mk("trace_malloc", "Malloc", "Trace malloc and free calls in libc.so.", "trace", "Runtime", "memory-stick", true),
		mk("trace_mount", "Mount", "Trace mount and unmount syscalls.", "trace", "Files", "folder-cog", true),
		mk("trace_oomkill", "OOM Kill", "Trace out-of-memory kill events.", "trace", "Runtime", "skull", true),
		mk("trace_open", "Open Files", "Trace file open events.", "trace", "Files", "file-search", true),
		mk("trace_signal", "Signal", "Trace signal activity.", "trace", "Runtime", "radio", true),
		mk("trace_sni", "SNI", "Trace TLS Server Name Indication values.", "trace", "Network", "badge-tls", true),
		mk("trace_ssl", "SSL", "Capture OpenSSL, GnuTLS, NSS, and libcrypto read/write data.", "trace", "Network", "lock-keyhole", true),
		mk("trace_tcp", "TCP", "Trace TCP connect, accept, and close activity.", "trace", "Network", "network", true,
			Option{Name: "connect-only", Type: "boolean", Description: "Only show connect events.", Default: false},
			Option{Name: "accept-only", Type: "boolean", Description: "Only show accept events.", Default: false},
			Option{Name: "failure-only", Type: "boolean", Description: "Only show failed events.", Default: false},
		),
		mk("trace_tcpdrop", "TCP Drops", "Trace TCP packets or segments dropped by the kernel.", "trace", "Network", "circle-off", true),
		mk("trace_tcpretrans", "TCP Retransmits", "Trace TCP retransmissions.", "trace", "Network", "repeat", true),
		mk("traceloop", "Trace Loop", "Capture syscalls in real time as a flight recorder.", "trace", "Runtime", "disc", true),
		mk("ttysnoop", "TTY Snoop", "Watch output from a tty or pts device.", "trace", "Runtime", "scan-eye", true),
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category == out[j].Category {
			return out[i].Name < out[j].Name
		}
		return out[i].Category < out[j].Category
	})
	for i := range out {
		out[i].EventSchema = eventSchemaForGadget(out[i].ID)
	}
	return out
}

func widgetForKind(kind string) string {
	switch kind {
	case "top":
		return "top"
	case "snapshot":
		return "snapshot"
	case "profile":
		return "profile"
	case "advise":
		return "report"
	case "audit", "trace":
		return "trace"
	default:
		return "table"
	}
}

func legacyCoreGadgetsForTests(tag string) []GadgetSpec {
	all := supportedGadgets(tag)
	ids := map[string]struct{}{
		"profile_cpu":      {},
		"snapshot_process": {},
		"trace_dns":        {},
		"trace_exec":       {},
		"trace_open":       {},
		"trace_tcp":        {},
	}
	out := make([]GadgetSpec, 0, len(ids))
	for _, gadget := range all {
		if _, ok := ids[gadget.ID]; ok {
			out = append(out, gadget)
		}
	}
	return out
}

func gadgetByID(id, tag string) (GadgetSpec, bool) {
	for _, g := range supportedGadgets(tag) {
		if g.ID == id {
			return g, true
		}
	}
	return GadgetSpec{}, false
}

func buildGadgetParams(target TraceTarget, options map[string]any) map[string]string {
	params := map[string]string{}
	if target.Namespace != "" {
		params["operator.KubeManager.namespace"] = target.Namespace
		params["namespace"] = target.Namespace
	}
	if target.Pod != "" {
		params["operator.KubeManager.podname"] = target.Pod
		params["podname"] = target.Pod
	}
	if target.Container != "" {
		params["operator.KubeManager.containername"] = target.Container
		params["containername"] = target.Container
	}
	if target.Node != "" {
		params["node"] = target.Node
	}
	if len(target.Selector) > 0 && target.Pod == "" {
		params["operator.KubeManager.selector"] = labelsParam(target.Selector)
		params["selector"] = labelsParam(target.Selector)
	}
	for k, v := range options {
		if k == "" || v == nil {
			continue
		}
		params[k] = fmt.Sprint(v)
	}
	return params
}

func labelsParam(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}
