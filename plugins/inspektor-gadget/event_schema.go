package main

type EventSchema struct {
	SourceStruct string        `json:"sourceStruct,omitempty"`
	Columns      []EventColumn `json:"columns,omitempty"`
}

type EventColumn struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Path        string `json:"path"`
	Kind        string `json:"kind,omitempty"`
	Description string `json:"description,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
}

func eventSchemaForGadget(id string) *EventSchema {
	if schema, ok := eventSchemas[id]; ok {
		return &schema
	}
	return &EventSchema{SourceStruct: "generic"}
}

func col(path, label, kind string) EventColumn {
	return EventColumn{Key: path, Path: path, Label: label, Kind: kind}
}

func hidden(path, label, kind string) EventColumn {
	c := col(path, label, kind)
	c.Hidden = true
	return c
}

func procCol() EventColumn {
	return col("proc", "Process", "process")
}

func percentCol(path, label string) EventColumn {
	return col(path, label, "percent")
}

var eventSchemas = map[string]EventSchema{
	"audit_seccomp": {
		SourceStruct: "auditSeccompEvent",
		Columns: []EventColumn{
			procCol(),
			col("syscall", "Syscall", "text"),
			col("code", "Code", "text"),
		},
	},
	"bpfstats": {
		SourceStruct: "ExpectedBpfstatsEvent",
		Columns: []EventColumn{
			col("gadgetName", "Gadget", "text"),
			col("gadgetImage", "Image", "text"),
			col("gadgetID", "Gadget ID", "text"),
			col("progName", "Program", "text"),
			col("progType", "Program Type", "text"),
			col("progID", "Program ID", "number"),
			col("mapCount", "Maps", "number"),
			col("mapMemory", "Map Memory", "bytes"),
			col("runcount", "Runs", "number"),
			col("runtime", "Runtime", "text"),
			col("comms", "Commands", "json"),
			col("pids", "PIDs", "json"),
		},
	},
	"fdpass": {
		SourceStruct: "ExpectedFdpassEvent",
		Columns: []EventColumn{
			procCol(),
			col("socket_ino", "Socket Inode", "number"),
			col("sockfd", "Socket FD", "number"),
			col("fd", "FD", "number"),
			col("file", "File", "text"),
		},
	},
	"fsnotify": {
		SourceStruct: "ExpectedFsnotifyEvent",
		Columns: []EventColumn{
			col("tracee_proc", "Tracee", "process"),
			col("tracer_proc", "Tracer", "process"),
			col("type", "Type", "text"),
			col("name", "Name", "text"),
			col("prio", "Priority", "number"),
			col("fa_mask", "Fanotify Mask", "text"),
			col("i_mask", "Inotify Mask", "text"),
			col("fa_type", "Fanotify Type", "text"),
			col("fa_pid", "Fanotify PID", "number"),
			col("fa_response", "Fanotify Response", "text"),
			col("i_wd", "Watch", "number"),
			col("i_cookie", "Cookie", "number"),
			col("i_ino", "Inode", "number"),
			col("i_ino_dir", "Dir Inode", "number"),
			hidden("tracee_mntns_id", "Tracee MntNS", "number"),
			hidden("tracer_mntns_id", "Tracer MntNS", "number"),
			hidden("tracee_uid", "Tracee UID", "number"),
			hidden("tracee_gid", "Tracee GID", "number"),
			hidden("tracer_uid", "Tracer UID", "number"),
			hidden("tracer_gid", "Tracer GID", "number"),
			hidden("fa_flags", "Fanotify Flags", "text"),
			hidden("fa_f_flags", "Fanotify F Flags", "text"),
		},
	},
	"profile_cpu": {
		SourceStruct: "profileCPUEntry",
		Columns: []EventColumn{
			col("proc.comm", "Command", "text"),
			col("samples", "Samples", "number"),
			col("user_stack", "User Stack", "json"),
			col("kern_stack", "Kernel Stack", "json"),
		},
	},
	"profile_cuda": {
		SourceStruct: "profileCUDAEntry",
		Columns: []EventColumn{
			col("proc.comm", "Command", "text"),
			col("count", "Count", "number"),
			col("size", "Size", "bytes"),
			col("ustack_raw.symbols", "User Stack", "json"),
		},
	},
	"profile_blockio": {
		SourceStruct: "profileBlockIOEntry",
		Columns: []EventColumn{
			col("latency", "Latency", "number"),
		},
	},
	"profile_tcprtt": {
		SourceStruct: "profileTCPRTTEntry",
		Columns: []EventColumn{
			col("latency", "Latency", "number"),
		},
	},
	"snapshot_file": {
		SourceStruct: "ExpectedSnapshotFileEvent",
		Columns: []EventColumn{
			col("comm", "Command", "text"),
			col("pid", "PID", "number"),
			col("tid", "TID", "number"),
			col("type", "Type", "text"),
			col("path", "Path", "text"),
			hidden("mntns_id", "MntNS", "number"),
		},
	},
	"snapshot_process": {
		SourceStruct: "snapshotProcessEntry",
		Columns: []EventColumn{
			col("comm", "Process", "command"),
			hidden("pid", "PID", "number"),
			col("tid", "TID", "number"),
			col("uid", "UID", "number"),
			col("gid", "GID", "number"),
			hidden("mntns_id", "MntNS", "number"),
		},
	},
	"snapshot_socket": {
		SourceStruct: "snapshotSocketEntry",
		Columns: []EventColumn{
			col("src", "Source", "endpoint"),
			col("dst", "Destination", "endpoint"),
			col("status", "Status", "text"),
			col("ino", "Inode", "number"),
			hidden("netns_id", "NetNS", "number"),
		},
	},
	"tcpdump": {
		SourceStruct: "tcpdumpEvent",
		Columns: []EventColumn{
			col("packet_type", "Packet Type", "text"),
		},
	},
	"top_blockio": {
		SourceStruct: "topBlockioEntry",
		Columns: []EventColumn{
			procCol(),
			col("rw", "RW", "text"),
			col("bytes", "Bytes", "bytes"),
			col("io", "I/O", "number"),
			col("us", "Latency", "number"),
			col("major", "Major", "number"),
			col("minor", "Minor", "number"),
		},
	},
	"top_file": {
		SourceStruct: "topFileEntry",
		Columns: []EventColumn{
			procCol(),
			col("file", "File", "text"),
			col("reads", "Reads", "number"),
			col("writes", "Writes", "number"),
			col("rbytes_raw", "Read Bytes", "bytes"),
			col("wbytes_raw", "Write Bytes", "bytes"),
			col("t", "Type", "text"),
			col("inode", "Inode", "number"),
			col("dev", "Device", "text"),
		},
	},
	"top_process": {
		SourceStruct: "topProcessEntry",
		Columns: []EventColumn{
			col("comm", "Process", "command"),
			hidden("pid", "PID", "number"),
			col("uid", "UID", "number"),
			col("state", "State", "text"),
			percentCol("cpuUsage", "CPU"),
			percentCol("cpuUsageRelative", "Rel CPU"),
			col("cpuTimeStr", "CPU Time", "text"),
			col("memoryRSS", "RSS", "bytes"),
			col("memoryVirtual", "Virtual", "bytes"),
			col("memoryShared", "Shared", "bytes"),
			percentCol("memoryRelative", "Memory"),
			col("threadCount", "Threads", "number"),
			hidden("priority", "Priority", "number"),
			hidden("nice", "Nice", "number"),
		},
	},
	"top_cpu_throttle": {
		SourceStruct: "topCPUThrottleEntry",
		Columns: []EventColumn{
			col("cgroupPath", "Cgroup", "text"),
			col("nrPeriods", "Periods", "number"),
			col("nrThrottled", "Throttled", "number"),
			col("throttledTime", "Throttled Time", "text"),
			percentCol("throttleRatio", "Throttled"),
			col("cpuQuota", "Quota", "number"),
			col("cpuPeriod", "Period", "number"),
			col("cpuLimitCores", "Limit", "number"),
			percentCol("psiSomeAvg10", "PSI 10s"),
			percentCol("psiSomeAvg60", "PSI 60s"),
		},
	},
	"top_tcp": {
		SourceStruct: "topTcpEntry",
		Columns: []EventColumn{
			col("comm", "Command", "text"),
			col("pid", "PID", "number"),
			col("src", "Source", "endpoint"),
			col("dst", "Destination", "endpoint"),
			col("sent", "Sent", "bytes"),
			col("received", "Received", "bytes"),
			hidden("tid", "TID", "number"),
			hidden("mntns_id", "MntNS", "number"),
		},
	},
	"trace_bind": {
		SourceStruct: "traceBindEvent",
		Columns: []EventColumn{
			procCol(),
			col("addr", "Address", "endpoint"),
			col("error", "Error", "text"),
			col("opts", "Options", "text"),
			col("bound_dev_if", "Bound Device", "number"),
		},
	},
	"trace_capabilities": {
		SourceStruct: "traceCapabilitiesEvent",
		Columns: []EventColumn{
			procCol(),
			col("cap", "Capability", "text"),
			col("cap_effective", "Effective", "text"),
			col("audit", "Audit", "text"),
			col("syscall", "Syscall", "text"),
			col("capable", "Capable", "boolean"),
			col("current_user_ns", "Current UserNS", "number"),
			col("target_user_ns", "Target UserNS", "number"),
			col("insetid", "In SetID", "boolean"),
			hidden("kstack", "Kernel Stack", "json"),
			hidden("ustack", "User Stack", "json"),
		},
	},
	"trace_dns": {
		SourceStruct: "traceDNSEvent",
		Columns: []EventColumn{
			procCol(),
			col("qr", "QR", "text"),
			col("name", "Name", "text"),
			col("qtype", "Query Type", "text"),
			col("rcode", "Response Code", "text"),
			col("src", "Source", "endpoint"),
			col("dst", "Destination", "endpoint"),
			col("nameserver", "Nameserver", "endpoint"),
			col("latency_ns_raw", "Latency", "number"),
			col("addresses", "Addresses", "json"),
			col("tc", "TC", "boolean"),
			col("rd", "RD", "boolean"),
			col("ra", "RA", "boolean"),
			hidden("id", "ID", "number"),
			hidden("netns_id", "NetNS", "number"),
			hidden("qtype_raw", "Query Type Raw", "number"),
			hidden("pkt_type", "Packet Type", "text"),
			hidden("rcode_raw", "Response Code Raw", "number"),
			hidden("qr_raw", "QR Raw", "number"),
		},
	},
	"trace_exec": {
		SourceStruct: "traceExecEvent",
		Columns: []EventColumn{
			procCol(),
			col("error", "Error", "text"),
			col("args", "Args", "text"),
			col("exepath", "Exe Path", "text"),
			col("file", "File", "text"),
			col("cwd", "CWD", "text"),
			col("loginuid", "Login UID", "number"),
			col("sessionid", "Session", "number"),
			col("upper_layer", "Upper Layer", "boolean"),
			col("from_rootfs", "RootFS", "boolean"),
			hidden("parent_exepath", "Parent Exe Path", "text"),
			hidden("dev_major", "Dev Major", "number"),
			hidden("dev_minor", "Dev Minor", "number"),
			hidden("inode", "Inode", "number"),
			hidden("ctime", "CTime", "number"),
			hidden("fctime", "File CTime", "number"),
			hidden("pctime", "Parent CTime", "number"),
			hidden("pupper_layer", "Parent Upper Layer", "boolean"),
			hidden("fupper_layer", "File Upper Layer", "boolean"),
			hidden("file_from_rootfs", "File RootFS", "boolean"),
		},
	},
	"trace_fsslower": {
		SourceStruct: "traceFSSlowerEvent",
		Columns: []EventColumn{
			procCol(),
			col("op", "Operation", "text"),
			col("file", "File", "text"),
			col("delta_us", "Latency", "number"),
			col("offset", "Offset", "number"),
			col("size", "Size", "bytes"),
		},
	},
	"trace_init_module": {
		SourceStruct: "ExpectedTraceInitModuleEvent",
		Columns: []EventColumn{
			procCol(),
			col("syscall", "Syscall", "text"),
			col("len", "Length", "bytes"),
			col("fd", "FD", "number"),
			col("filepath", "File Path", "text"),
			col("flags", "Flags", "text"),
			col("param_values", "Parameters", "text"),
		},
	},
	"trace_mount": {
		SourceStruct: "traceMountEvent",
		Columns: []EventColumn{
			procCol(),
			col("op", "Operation", "text"),
			col("call", "Call", "text"),
			col("src", "Source", "text"),
			col("dest", "Destination", "text"),
			col("fs", "Filesystem", "text"),
			col("flags", "Flags", "text"),
			col("error", "Error", "text"),
			col("delta_raw", "Latency", "number"),
			hidden("data", "Data", "text"),
		},
	},
	"trace_oomkill": {
		SourceStruct: "traceOomKillEvent",
		Columns: []EventColumn{
			col("fprocess", "Victim Process", "text"),
			col("tcomm", "Trigger Command", "text"),
			col("tpid", "Trigger PID", "number"),
			col("pages", "Pages", "number"),
			hidden("tmntns_id", "Target MntNS", "number"),
		},
	},
	"trace_open": {
		SourceStruct: "traceOpenEvent",
		Columns: []EventColumn{
			procCol(),
			col("fname", "File Name", "text"),
			col("fpath", "File Path", "text"),
			col("fd", "FD", "number"),
			col("flags", "Flags", "text"),
			col("mode", "Mode", "text"),
			col("error", "Error", "text"),
			hidden("flags_raw", "Flags Raw", "number"),
			hidden("mode_raw", "Mode Raw", "number"),
			hidden("error_raw", "Error Raw", "number"),
		},
	},
	"trace_signal": {
		SourceStruct: "traceSignalEvent",
		Columns: []EventColumn{
			procCol(),
			col("sig", "Signal", "text"),
			col("error", "Error", "text"),
			hidden("sig_raw", "Signal Raw", "number"),
		},
	},
	"trace_sni": {
		SourceStruct: "traceSNIEvent",
		Columns: []EventColumn{
			procCol(),
			col("name", "Server Name", "text"),
			hidden("netns_id", "NetNS", "number"),
		},
	},
	"trace_tcp": {
		SourceStruct: "traceTCPEvent",
		Columns: []EventColumn{
			procCol(),
			col("type", "Type", "text"),
			col("src", "Source", "endpoint"),
			col("dst", "Destination", "endpoint"),
			col("error", "Error", "text"),
			col("fd", "FD", "number"),
			col("accept_fd", "Accept FD", "number"),
			hidden("netns_id", "NetNS", "number"),
		},
	},
	"trace_tcpretrans": {
		SourceStruct: "traceTCPretransEvent",
		Columns: []EventColumn{
			procCol(),
			col("type", "Type", "text"),
			col("src", "Source", "endpoint"),
			col("dst", "Destination", "endpoint"),
			hidden("netns_id", "NetNS", "number"),
		},
	},
	"traceloop": {
		SourceStruct: "traceloopEvent",
		Columns: []EventColumn{
			col("comm", "Command", "text"),
			col("pid", "PID", "number"),
			col("cpu", "CPU", "number"),
			col("syscall", "Syscall", "text"),
			col("parameters", "Parameters", "json"),
			col("ret", "Return", "number"),
			hidden("mntns_id", "MntNS", "number"),
		},
	},
	"ttysnoop": {
		SourceStruct: "ExpectedTtysnoopEvent",
		Columns: []EventColumn{
			procCol(),
			col("len", "Length", "bytes"),
			col("buf", "Buffer", "text"),
		},
	},
}
