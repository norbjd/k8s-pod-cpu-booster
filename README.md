# k8s-pod-cpu-booster

Simple PoC to give pods a CPU boost during startup (before pod is `Ready`).

:warning: **this is pre-alpha / work in progress, don't use in production** :warning:

- supports Kubernetes clusters starting from version 1.27 only with `InPlacePodVerticalScaling` feature gate enabled (for older versions, see [`v0.1.0`](https://github.com/norbjd/k8s-pod-cpu-booster/tree/v0.1.0))
- requires container to boost to have:
  - a `readinessProbe` configured
  - a value for `resources.limits.cpu`
  - a `resizePolicy` with `resourceName: cpu` and `restartPolicy: NotRequire`
- works with pods with multiple containers, but can **only** boost a **single** container inside the pod

Between startup and `Ready` status, the container benefits from a CPU boost (x10).

## How does it work?

It is deployed in two parts:

- a mutating webhook boosting the CPU of pods with `norbjd.github.io/k8s-pod-cpu-booster-enabled: "true"` label, before they are submitted to k8s API
- a controller listening for every update of pods with `norbjd.github.io/k8s-pod-cpu-booster-enabled: "true"` label; when a pod is ready, it will reset its CPU limit

The CPU boost can be configured with `norbjd.github.io/k8s-pod-cpu-booster-multiplier` label:

- if specified, it will increase the CPU limit by `x`, where `x` is the value of the label (must be an unsigned integer)
- if unspecified or invalid, it will increase the CPU limit by the default value (`10`)

## Install

Use `ko`. Example on a `kind` cluster:

```sh
make --directory config/ mutating-webhook-certs # generates self-signed certificates for the webhook
kustomize build config/ | KO_DOCKER_REPO=kind.local ko apply -f -
```

## Test/Demo

Create a `kind` cluster:

```sh
kind create cluster --config examples/kind-config.yaml
```

Load `python:3.11-alpine` image on the cluster (not mandatory):

```sh
docker pull python:3.11-alpine
kind load docker-image python:3.11-alpine
```

Install `k8s-pod-cpu-booster`:

```sh
make --directory config/ mutating-webhook-certs # generates self-signed certificates for the webhook
kustomize build config/ | KO_DOCKER_REPO=kind.local ko apply -f -
```

Start two similar pods with low CPU limits and running `python -m http.server`, with a readiness probe configured to check when the http server is started. The only differences are the name (obviously), and the label `norbjd.github.io/k8s-pod-cpu-booster-enabled`:

```diff
--- examples/pod-no-boost.yaml
+++ examples/pod-with-default-boost.yaml
@@ -4 +4,3 @@
-  name: pod-no-boost
+  labels:
+    norbjd.github.io/k8s-pod-cpu-booster-enabled: "true"
+  name: pod-with-default-boost
```

> [!NOTE]
> The CPU boost multiplier can also be configured (see [`pod-with-small-boost.yaml`](https://github.com/norbjd/k8s-pod-cpu-booster/blob/main/examples/pod-with-small-boost.yaml)) by using the `norbjd.github.io/k8s-pod-cpu-booster-multiplier` label.

As a result, the pod `pod-with-default-boost` (with the label) will benefit from a CPU boost, but `pod-no-boost` won't:

```sh
kubectl apply -f examples/pod-no-boost.yaml -f examples/pod-with-default-boost.yaml

# wait until pods are ready
kubectl wait --for=condition=Ready pod/pod-with-default-boost pod/pod-no-boost
```

Once both are ready, check how much time each took to be ready:

```sh
for pod_name in pod-with-default-boost pod-no-boost
do
    kubectl get pods $pod_name -o go-template='{{ .metadata.name }}{{ " " }}{{ range .status.containerStatuses }}{{ if eq .name "python" }}{{ "started at: " }}{{ .state.running.startedAt }}{{ end }}{{ end }}{{ " / " }}{{ range .status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ "ready at: " }}{{ .lastTransitionTime }}{{ end }}{{ end }}{{ "\n" }}'
done
```

Example result:

```
pod-with-default-boost started at: 2023-10-28T14:00:46Z / ready at: 2023-10-28T14:00:49Z
pod-no-boost started at: 2023-10-28T14:00:46Z / ready at: 2023-10-28T14:01:04Z
```

Here, the pod with the CPU boost (`pod-with-default-boost`) took around 3 seconds to start, while the pod without CPU boost (`pod-no-boost`) took around 18 seconds.

> [!NOTE]
> Even if boosts are made at `Pod` level and not at `Deployment` level, this works too with `Deployment`s (and probably other resources managing pods under the hood). See [`examples/deployment-with-default-boost.yaml`](https://github.com/norbjd/k8s-pod-cpu-booster/blob/main/examples/deployment-with-default-boost.yaml) for example.

Cleanup:

```sh
kubectl delete -f examples/pod-no-boost.yaml -f examples/pod-with-default-boost.yaml

kustomize build config/ | KO_DOCKER_REPO=kind.local ko delete -f -
make --directory config/ remove-certs

kind delete cluster
```

## TODO

- support pods with multiple containers
