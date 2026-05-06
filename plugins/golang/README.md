# Golang Diagnostics Plugin

The `golang` plugin inspects Kubernetes workloads that already expose Go
runtime diagnostics. It does not patch deployments, inject agents, restart
pods, or enable mutating runtime actions.

## Workload setup

For gops, start the agent on a localhost-only port. A fixed port is simplest:

```go
agent.Listen(agent.Options{
	Addr:      "127.0.0.1:6061",
	ConfigDir: "/tmp/gops",
})
```

Random ports also work if the port file is readable through Kubernetes exec:

```go
agent.Listen(agent.Options{
	Addr:      "127.0.0.1:0",
	ConfigDir: "/tmp/gops",
})
```

For pprof, mount the standard handlers on a localhost-only admin listener, for
example `127.0.0.1:6060/debug/pprof`.

If no explicit ports are provided, the plugin tries readable gops port files,
then configured/default gops ports. It also probes `/debug/pprof/` on declared
Kubernetes `containerPort` values for the selected container.

The plugin reaches these localhost-only ports with Kubernetes pod port-forward,
so the target process does not need to bind to `0.0.0.0`.

## Build and test

```sh
task -d plugins/golang test
task build:plugin:golang
kubectl apply -f plugins/golang/Plugin.yaml
```
