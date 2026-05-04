# Kubernetes Logs Plugin

Reference plugin: streams logs from a Pod, Deployment, StatefulSet, DaemonSet,
ReplicaSet, Job, or CronJob, walking owner references to fan out across every
matching pod.

## What it shows the SDK author

- Reading the catalog item (`Host.GetConfigItem`) to learn `kind / namespace / name`.
- Resolving a Kubernetes connection by type (`Host.GetConnection(ctx, "kubernetes", configID)`).
- An iframe UI that calls back into the host's operation API for `list-pods`,
  then opens a Server-Sent Events stream against the plugin's own HTTP server
  for follow-mode log tailing.
- Both the gRPC operation contract (`tail`, `list-pods`) and a non-trivial HTTP
  contract (`/api/logs` SSE) coexisting on the same plugin port.

## Build & install

```sh
mkdir -p $MISSION_CONTROL_PLUGIN_PATH
go build -o $MISSION_CONTROL_PLUGIN_PATH/kubernetes-logs ./plugins/kubernetes-logs
kubectl apply -f plugins/kubernetes-logs/Plugin.yaml
```

## CLI

```sh
# Tail the last 100 lines from every pod owned by a Deployment:
mission-control plugin kubernetes-logs tail \
  --config-id <deployment-config-uuid> \
  --param tailLines=100

# Just resolve which pods a workload maps to:
mission-control plugin kubernetes-logs list-pods --config-id <uuid>
```

## HTTP

```sh
curl -X POST -d '{"tailLines":50}' \
  "$MISSION_CONTROL_URL/api/plugins/kubernetes-logs/operations/tail?config_id=<uuid>"
```

Returns `application/clicky+json` rows of `{pod, container, line}`.

## Iframe UI

Open any matching catalog item — the **Logs** tab opens an embedded terminal-style
viewer with pod/container selectors and follow-mode streaming.

## Connection resolution

The plugin's `connections.kubernetes` field is the same shape as the
`exec` action's: it can be a kubeconfig env-var, a named `Connection`,
`fromConfigItem` (derive credentials from the catalog item being viewed —
the natural choice for an EKS / GKE / Kubernetes cluster config item), or
left empty so the plugin falls back to its own in-cluster service account.
