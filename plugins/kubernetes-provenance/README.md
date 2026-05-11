# Kubernetes Provenance Plugin

Explains where a Kubernetes object came from and what manages it.

## Operation

```sh
mission-control plugin kubernetes-provenance explain --config-id <kubernetes-config-id>
```

Optional params:

```json
{
  "detectors": ["runtime", "argo", "flux", "helm", "kubectl"],
  "includeEvidence": true,
  "includeManagedFields": true,
  "maxOwnerDepth": 5
}
```

The response contains the selected object, a summary, runtime owner chain, detected controllers, sources, renderers, field writers, and evidence.

## Build & install

```sh
mkdir -p $MISSION_CONTROL_PLUGIN_PATH
go build \
  -ldflags "-X 'main.Version=0.1.0' -X 'main.BuildDate=$(date '+%Y-%m-%d %H:%M:%S')'" \
  -o $MISSION_CONTROL_PLUGIN_PATH/kubernetes-provenance \
  ./plugins/kubernetes-provenance
kubectl apply -f plugins/kubernetes-provenance/Plugin.yaml
```
