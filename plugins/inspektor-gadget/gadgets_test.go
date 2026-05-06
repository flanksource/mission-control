package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("gadget parameters", func() {
	ginkgo.It("lists all image-based gadgets from the latest docs", func() {
		gadgets := supportedGadgets(defaultIGTag)
		ids := make([]string, 0, len(gadgets))
		for _, gadget := range gadgets {
			ids = append(ids, gadget.ID)
			Expect(gadget.Image).To(ContainSubstring("/gadget/" + gadget.ID + ":" + defaultIGTag))
			Expect(gadget.Icon).ToNot(BeEmpty())
			Expect(gadget.DocsURL).To(ContainSubstring("/docs/latest/gadgets/" + gadget.ID + "/"))
		}
		Expect(ids).To(ConsistOf(
			"advise_networkpolicy",
			"advise_seccomp",
			"audit_seccomp",
			"bpfstats",
			"deadlock",
			"fdpass",
			"fsnotify",
			"profile_blockio",
			"profile_cpu",
			"profile_cuda",
			"profile_qdisc_latency",
			"profile_tcprtt",
			"snapshot_file",
			"snapshot_process",
			"snapshot_socket",
			"tcpdump",
			"top_blockio",
			"top_cpu_throttle",
			"top_file",
			"top_process",
			"top_tcp",
			"trace_bind",
			"trace_capabilities",
			"trace_dns",
			"trace_exec",
			"trace_fsslower",
			"trace_init_module",
			"trace_lsm",
			"trace_malloc",
			"trace_mount",
			"trace_oomkill",
			"trace_open",
			"trace_signal",
			"trace_sni",
			"trace_ssl",
			"trace_tcp",
			"trace_tcpdrop",
			"trace_tcpretrans",
			"traceloop",
			"ttysnoop",
		))
	})

	ginkgo.It("builds stable Kubernetes filters for a pod target", func() {
		params := buildGadgetParams(TraceTarget{
			Namespace: "default",
			Pod:       "api-123",
			Container: "api",
			Node:      "node-a",
		}, map[string]any{"paths": true})

		Expect(params).To(HaveKeyWithValue("namespace", "default"))
		Expect(params).To(HaveKeyWithValue("podname", "api-123"))
		Expect(params).To(HaveKeyWithValue("containername", "api"))
		Expect(params).To(HaveKeyWithValue("node", "node-a"))
		Expect(params).To(HaveKeyWithValue("paths", "true"))
	})

	ginkgo.It("accepts gadget arguments from maps and cli-style lines", func() {
		options, err := normalizeTraceOptions(TraceStartParams{
			Options:   map[string]any{"paths": true},
			Arguments: map[string]any{"filter": `proc.comm == "curl"`},
			Args:      []string{"--operator.Sort.sort=timestamp", "custom-flag"},
			ArgString: "operator.Limiter.max-entries=50\n# ignored\nsample-rate: 99",
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(options).To(HaveKeyWithValue("paths", true))
		Expect(options).To(HaveKeyWithValue("filter", `proc.comm == "curl"`))
		Expect(options).To(HaveKeyWithValue("operator.Sort.sort", "timestamp"))
		Expect(options).To(HaveKeyWithValue("custom-flag", true))
		Expect(options).To(HaveKeyWithValue("operator.Limiter.max-entries", "50"))
		Expect(options).To(HaveKeyWithValue("sample-rate", "99"))
	})

	ginkgo.It("serializes selectors deterministically", func() {
		Expect(labelsParam(map[string]string{"tier": "api", "app": "shop"})).To(Equal("app=shop,tier=api"))
	})
})
