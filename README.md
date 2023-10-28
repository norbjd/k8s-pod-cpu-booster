# k8s-pod-cpu-booster

Simple PoC to give pods a CPU boost during startup (before pod is `Ready`).

:warning: **this is pre-alpha / work in progress, don't use in production** :warning:

- supports [kind](https://kind.sigs.k8s.io/) and [kapsule](https://www.scaleway.com/en/kubernetes-kapsule/) clusters
- requires pods to have a `readinessProbe` configured, and a value for `resources.limits.cpu`
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
--- examples/pod-no-boost.yaml
+++ examples/pod-with-default-boost.yaml
@@ -4 +4,3 @@
-  name: pod-no-boost
+  annotations:
+    norbjd.github.io/k8s-pod-cpu-booster-enabled: "true"
+  name: pod-with-default-boost
```

> [!NOTE]
> The CPU boost multiplier can also be configured (see [`pod-with-small-boost.yaml`](https://github.com/norbjd/k8s-pod-cpu-booster/blob/main/examples/pod-with-small-boost.yaml)) by using the `norbjd.github.io/k8s-pod-cpu-booster-multiplier` annotation.

As a result, the pod `pod-with-default-boost` (with the annotation) will benefit from a CPU boost, but `pod-no-boost` won't:

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

KO_DOCKER_REPO=kind.local ko delete -f config/

kind delete cluster
```

## TODO

- support pods with multiple containers
