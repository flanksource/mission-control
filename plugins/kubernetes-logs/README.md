# Kubernetes Logs Plugin

Reference plugin: returns logs from a Pod, Deployment, StatefulSet, DaemonSet,
ReplicaSet, Job, or CronJob, walking owner references to fan out across every
matching pod.

## What it shows the SDK author

- Reading the catalog item via `Host.GetConfigItem` to learn `kind / namespace / name`.
- Resolving a Kubernetes connection via `Host.GetConnectionForConfig`.
- Exposing operation handlers over the gRPC plugin contract (`tail`, `list-pods`).

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
  "$MISSION_CONTROL_URL/api/plugins/kubernetes-logs/invoke/tail?config_id=<uuid>"
```

Returns `application/clicky+json` rows of `{pod, container, line}`.

## Connection resolution

The plugin asks Mission Control for the connection used by the scraper that
created the config item. If Mission Control cannot resolve one, the plugin falls
back to its own in-cluster service account.
