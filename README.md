# k8s-pod-cpu-booster

Simple PoC to give pods a CPU boost during startup (before pod is `Ready`).

:warning: **this is pre-alpha / work in progress, don't use in production** :warning:

- only tested on a kind cluster with cgroup v2 + containerd
- require pods to have a `readinessProbe` configured, and a value for `resources.limits.cpu`
- only works with pods with a single container

Between startup and `Ready` status, the pod benefits from a CPU boost (x10).

## How does it work?

It is deployed as a controller on every node (with a `DaemonSet`). It listens for every pod update; if a pod has `norbjd.github.io/k8s-pod-cpu-booster-enabled: "true"` label: it boosts the CPU at pod startup, and reset the CPU limit when the pod is ready.

The CPU boost can be configured with `norbjd.github.io/k8s-pod-cpu-booster-multiplier` annotation:

- if specified, it will increase the CPU limit by `x`, where `x` is the value of the annotation (must be an unsigned integer)
- if unspecified or invalid, it will increase the CPU limit by the default value (`10`)

The controller messes with cgroups file `cpu.max` to give that boost (or reset the limit).

## Install

Use `ko`. Example on a `kind` cluster:

```sh
KO_DOCKER_REPO=kind.local ko apply -f config/
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
KO_DOCKER_REPO=kind.local ko apply -f config/
```

Start two similar pods with low CPU limits and running `python -m http.server`, with a readiness probe configured to check when the http server is started. The only differences are the name (obviously), and the annotation `norbjd.github.io/k8s-pod-cpu-booster-enabled`:

```diff
--- examples/python-no-boost.yaml
+++ examples/python-with-default-boost.yaml
@@ -4 +4,3 @@
-  name: python-no-boost
+  annotations:
+    norbjd.github.io/k8s-pod-cpu-booster-enabled: "true"
+  name: python-with-default-boost
```

> [!NOTE]
> The CPU boost multiplier can also be configured (see [`python-with-small-boost.yaml`](examples/python-with-small-boost.yaml)) by using the `norbjd.github.io/k8s-pod-cpu-booster-multiplier` annotation.

As a result, the pod `python-with-default-boost` (with the annotation) will benefit from a CPU boost, but `python-no-boost` won't:

```sh
kubectl apply -f examples/python-no-boost.yaml -f examples/python-with-default-boost.yaml

# wait until pods are ready
kubectl wait --for=condition=Ready pod/python-with-default-boost pod/python-no-boost
```

Once both are ready, check how much time each took to be ready:

```sh
for pod_name in python-with-default-boost python-no-boost
do
    kubectl get pods $pod_name -o go-template='{{ .metadata.name }}{{ " " }}{{ range .status.containerStatuses }}{{ if eq .name "python" }}{{ "started at: " }}{{ .state.running.startedAt }}{{ end }}{{ end }}{{ " / " }}{{ range .status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ "ready at: " }}{{ .lastTransitionTime }}{{ end }}{{ end }}{{ "\n" }}'
done
```

Example result:

```
python-with-default-boost started at: 2023-09-03T15:55:45Z / ready at: 2023-09-03T15:55:46Z
python-no-boost started at: 2023-09-03T15:55:44Z / ready at: 2023-09-03T15:55:58Z
```

Here, the pod with the CPU boost (`python-with-default-boost`) took around 1 second to start, while the pod without CPU boost (`python-no-boost`) took around 14 seconds.

Cleanup:

```sh
kubectl delete -f examples/python-no-boost.yaml -f examples/python-with-default-boost.yaml

KO_DOCKER_REPO=kind.local ko delete -f config/

kind delete cluster
```

## TODO

- support other things than containerd and cgroups v2
- support pods with multiple containers
