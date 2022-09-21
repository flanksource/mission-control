# setup helm (macos)
`brew install helm`

`helm dependecy build ./chart`

`helm template incident-commander ./chart`



# Local kind cluster setup with incident-commander chart.

This setup creates a kind cluster with nginx controller and support for a local docker repository.

```bash
#!/bin/sh
set -o errexit

# create registry container unless it already exists
reg_name='kind-registry'
reg_port='5001'
if [ "$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)" != 'true' ]; then
  docker run \
    -d --restart=always -p "127.0.0.1:${reg_port}:5000" --name "${reg_name}" \
    registry:2
fi

# create a cluster with the local registry enabled in containerd
cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${reg_port}"]
    endpoint = ["http://${reg_name}:5000"]
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 80
        hostPort: 80
        protocol: TCP
      - containerPort: 443
        hostPort: 443
        protocol: TCP
EOF

# connect the registry to the cluster network if not already connected
if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]; then
  docker network connect "kind" "${reg_name}"
fi

# Document the local registry
# https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${reg_port}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF


# Install nginx ingress controller
kubectl apply --filename https://raw.githubusercontent.com/kubernetes/ingress-nginx/master/deploy/static/provider/kind/deploy.yaml

# Wait for controller to be ready
kubectl wait --namespace ingress-nginx --for=condition=ready pod --selector=app.kubernetes.io/component=controller --timeout=90s
```


Once cluster is created, you can push images to `reposity:5001/<image_name>`. Do configure values files

e.g
```diff
modified   chart/values.yaml
@@ -8,7 +8,7 @@ replicas: 1
 nameOverride: ""

 image:
-  repository: docker.io/flanksource/incident-commander
+  repository: localhost:5001/incident-commander
   pullPolicy: IfNotPresent
   tag: "latest"

@@ -88,6 +88,11 @@ uiHost: "http://incident-manager-ui.canary.labs.flanksource.com"
 kratosURI: http://incident-commander:8080/kratos/

 incident-manager-ui:
+  image:
+    repositoryPrefix: "localhost:5001" # Repository prefix, without trailing /
+    pullPolicy: IfNotPresent
+    # Overrides the image tag whose default is the chart appVersion.
+    tag: "latest"
   nameOverride: "incident-manager-ui"
   fullnameOverride: "incident-manager-ui"
   oryKratosURI: http://incident-commander:8080/kratos/
```


Some of the components depend on config, which is set via aws-sandbox. We need to explicitly apply them to the cluster. To use the configs form aws-sandbox, first fix the config for config-db-rules.yaml and remove the aws check.

```diff
modified   spec/canaries/_tenant/config-db-rules.yaml
@@ -3,12 +3,4 @@ kind: ConfigMap
 metadata:
   name: config-db-rules
 data:
-  config.yaml: |
-    schedule: "@every 60m"
-    aws:
-      - region: eu-west-2
-        compliance: true
-        patch_states: true
-        trusted_advisor_check: false
-        patch_details: true
-        inventory: true
\ No newline at end of file
+  config.yaml: ""
```

Apply configs to the cluster.

```bash
kubectl apply -f ../aws-sandbox/spec/canaries/_tenant/postgres-conf.yaml
kubectl apply -f ../aws-sandbox/spec/canaries/_tenant/config-db-rules.yaml
```


Now you are ready to apply the chart to the cluster.

```bash
helm upgrade --debug --install -f ./chart/values.yaml incident-manager ./chart
```

You can generate templates for specific files, for testing. e.g

```bash
helm template -f ./chart/values.yaml --debug incident-manager ./chart -s templates/kratos-config.yaml
```
