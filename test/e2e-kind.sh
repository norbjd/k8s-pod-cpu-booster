#!/usr/bin/env bash

set -euo pipefail

echo "Running on OS: $OS" # this will fail if not set because of set -u
echo "System information (uname -a): $(uname -a)"

docker pull python:3.11-alpine
kind load docker-image python:3.11-alpine

kubectl apply \
    -f ./test/e2e/pod-no-boost.yaml \
    -f ./test/e2e/pod-with-default-boost.yaml \
    -f ./test/e2e/deployment-no-boost.yaml \
    -f ./test/e2e/deployment-with-default-boost.yaml

kubectl wait --for=condition=Ready pod/pod-with-default-boost pod/pod-no-boost
kubectl wait --for=condition=Available deployment/deployment-with-default-boost deployment/deployment-no-boost

pod_with_boost_start_time=$(kubectl get pods pod-with-default-boost -o go-template='{{ range .status.containerStatuses }}{{ if eq .name "python" }}{{ .state.running.startedAt }}{{ end }}{{ end }}')
pod_with_boost_ready_time=$(kubectl get pods pod-with-default-boost -o go-template='{{ range .status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ .lastTransitionTime }}{{ end }}{{ end }}')
pod_with_boost_seconds_to_be_ready=$(( $( date -d "$pod_with_boost_ready_time" +%s ) - $( date -d "$pod_with_boost_start_time" +%s ) ))
echo "[INFO] pod-with-default-boost pod took $pod_with_boost_seconds_to_be_ready second(s) to be ready"

pod_no_boost_start_time=$(kubectl get pods pod-no-boost -o go-template='{{ range .status.containerStatuses }}{{ if eq .name "python" }}{{ .state.running.startedAt }}{{ end }}{{ end }}')
pod_no_boost_ready_time=$(kubectl get pods pod-no-boost -o go-template='{{ range .status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ .lastTransitionTime }}{{ end }}{{ end }}')
pod_no_boost_seconds_to_be_ready=$(( $( date -d "$pod_no_boost_ready_time" +%s ) - $( date -d "$pod_no_boost_start_time" +%s ) ))
echo "[INFO] pod-no-boost pod took $pod_no_boost_seconds_to_be_ready second(s) to be ready"

# for pods managed by a deployment, the following kubectl commands work because we have only 1 replica
deployment_with_boost_pod_start_time=$(kubectl get pods -l app=deployment-with-default-boost -o go-template='{{ range (index .items 0).status.containerStatuses }}{{ if eq .name "python" }}{{ .state.running.startedAt }}{{ end }}{{ end }}')
deployment_with_boost_pod_ready_time=$(kubectl get pods -l app=deployment-with-default-boost -o go-template='{{ range (index .items 0).status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ .lastTransitionTime }}{{ end }}{{ end }}')
deployment_with_boost_pod_seconds_to_be_ready=$(( $( date -d "$deployment_with_boost_pod_ready_time" +%s ) - $( date -d "$deployment_with_boost_pod_start_time" +%s ) ))
echo "[INFO] pod from deployment-with-default-boost deployment took $deployment_with_boost_pod_seconds_to_be_ready second(s) to be ready"

deployment_no_boost_pod_start_time=$(kubectl get pods -l app=deployment-no-boost -o go-template='{{ range (index .items 0).status.containerStatuses }}{{ if eq .name "python" }}{{ .state.running.startedAt }}{{ end }}{{ end }}')
deployment_no_boost_pod_ready_time=$(kubectl get pods -l app=deployment-no-boost -o go-template='{{ range (index .items 0).status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ .lastTransitionTime }}{{ end }}{{ end }}')
deployment_no_boost_pod_seconds_to_be_ready=$(( $( date -d "$deployment_no_boost_pod_ready_time" +%s ) - $( date -d "$deployment_no_boost_pod_start_time" +%s ) ))
echo "[INFO] pod from deployment-no-boost deployment took $deployment_no_boost_pod_seconds_to_be_ready second(s) to be ready"

exit_code=0

# pods with default boosts should start <ready_time_minimum_ratio> times quicker than pods with no boost
ready_time_minimum_ratio=2

if [ $(( $pod_no_boost_seconds_to_be_ready / $pod_with_boost_seconds_to_be_ready )) -ge $ready_time_minimum_ratio ]
then
    echo -e "\033[0;32m[SUCCESS]\033[0m pod-with-default-boost started more than $ready_time_minimum_ratio times quicker than pod-no-boost"
else
    echo -e "\033[0;31m[FAILURE]\033[0m pod-with-default-boost should start more than $ready_time_minimum_ratio times quicker than pod-no-boost"
    exit_code=1
fi

if [ $(( $deployment_no_boost_pod_seconds_to_be_ready / $deployment_with_boost_pod_seconds_to_be_ready )) -ge $ready_time_minimum_ratio ]
then
    echo -e "\033[0;32m[SUCCESS]\033[0m pods managed by deployment-with-default-boost started more than $ready_time_minimum_ratio times quicker than pods managed by deployment-no-boost"
else
    echo -e "\033[0;31m[FAILURE]\033[0m pods managed by deployment-with-default-boost should start more than $ready_time_minimum_ratio times quicker than pods managed by deployment-no-boost"
    exit_code=1
fi

# also check that cgroup cpu.max or cpu.cfs_quota_us file is back to the default limits
pod_with_boost_pod_uid=$(kubectl get pods pod-with-default-boost -o jsonpath='{.metadata.uid}' | sed 's~-~_~g')
pod_with_boost_python_container_id=$(kubectl get pods pod-with-default-boost -o jsonpath='{.status.containerStatuses[?(@.name=="python")].containerID}' | cut -d '/' -f 3)
deployment_with_boost_pod_uid=$(kubectl get pods -l app=deployment-with-default-boost -o jsonpath='{.items[0].metadata.uid}' | sed 's~-~_~g')
deployment_with_boost_pod_python_container_id=$(kubectl get pods -l app=deployment-with-default-boost -o jsonpath='{.items[0].status.containerStatuses[?(@.name=="python")].containerID}' | cut -d '/' -f 3)

if [ "$OS" == "ubuntu-20.04" ] # cgroup v1
then
    docker exec kind-worker cat /sys/fs/cgroup/cpu,cpuacct/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${pod_with_boost_pod_uid}.slice/cpu.cfs_quota_us > pod_cpu.cfs_quota_us
    docker exec kind-worker cat /sys/fs/cgroup/cpu,cpuacct/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${pod_with_boost_pod_uid}.slice/cri-containerd-${pod_with_boost_python_container_id}.scope/cpu.cfs_quota_us > pod_container_cpu.cfs_quota_us

    if ! diff -b <(cat pod_cpu.cfs_quota_us) <(echo "5000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m pod cgroup cpu.cfs_quota_us has not been reset"
        exit_code=1
    fi

    if ! diff -b <(cat pod_container_cpu.cfs_quota_us) <(echo "5000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m python container cgroup cpu.cfs_quota_us has not been reset"
        exit_code=1
    fi

    docker exec kind-worker cat /sys/fs/cgroup/cpu,cpuacct/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${deployment_with_boost_pod_uid}.slice/cpu.cfs_quota_us > deployment_pod_cpu.cfs_quota_us
    docker exec kind-worker cat /sys/fs/cgroup/cpu,cpuacct/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${deployment_with_boost_pod_uid}.slice/cri-containerd-${deployment_with_boost_pod_python_container_id}.scope/cpu.cfs_quota_us > deployment_pod_container_cpu.cfs_quota_us

    if ! diff -b <(cat deployment_pod_cpu.cfs_quota_us) <(echo "5000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m pod (managed by deployment) cgroup cpu.cfs_quota_us has not been reset"
        exit_code=1
    fi

    if ! diff -b <(cat deployment_pod_container_cpu.cfs_quota_us) <(echo "5000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m python container (in pod managed by deployment) cgroup cpu.cfs_quota_us has not been reset"
        exit_code=1
    fi
else # cgroup v2
    docker exec kind-worker cat /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${pod_with_boost_pod_uid}.slice/cpu.max > pod_cpu.max
    docker exec kind-worker cat /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${pod_with_boost_pod_uid}.slice/cri-containerd-${pod_with_boost_python_container_id}.scope/cpu.max > pod_container_cpu.max

    if ! diff -b <(cat pod_cpu.max) <(echo "5000 100000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m pod cgroup cpu.max has not been reset"
        exit_code=1
    fi

    if ! diff -b <(cat pod_container_cpu.max) <(echo "5000 100000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m python container cgroup cpu.max has not been reset"
        exit_code=1
    fi

    docker exec kind-worker cat /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${deployment_with_boost_pod_uid}.slice/cpu.max > deployment_pod_cpu.max
    docker exec kind-worker cat /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod${deployment_with_boost_pod_uid}.slice/cri-containerd-${deployment_with_boost_pod_python_container_id}.scope/cpu.max > deployment_pod_container_cpu.max

    if ! diff -b <(cat deployment_pod_cpu.max) <(echo "5000 100000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m pod (managed by deployment) cgroup cpu.max has not been reset"
        exit_code=1
    fi

    if ! diff -b <(cat deployment_pod_container_cpu.max) <(echo "5000 100000")
    then
        echo -e "\033[0;31m[FAILURE]\033[0m python container (in pod managed by deployment) cgroup cpu.max has not been reset"
        exit_code=1
    fi
fi

echo "mutating-webhook logs"
echo "===================="
kubectl logs --tail=-1 -n pod-cpu-booster -l app=mutating-webhook --prefix
echo "===================="

echo "pod-cpu-boost-reset logs"
echo "===================="
kubectl logs --tail=-1 -n pod-cpu-booster -l app=pod-cpu-boost-reset --prefix
echo "===================="

kubectl delete \
    -f ./test/e2e/pod-no-boost.yaml \
    -f ./test/e2e/pod-with-default-boost.yaml \
    -f ./test/e2e/deployment-no-boost.yaml \
    -f ./test/e2e/deployment-with-default-boost.yaml

exit $exit_code
