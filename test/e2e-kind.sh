#!/usr/bin/env bash

set -euo pipefail

docker pull python:3.11-alpine
kind load docker-image python:3.11-alpine

kubectl apply \
    -f ./test/e2e/python-no-boost.yaml \
    -f ./test/e2e/python-with-boost.yaml

kubectl wait --for=condition=Ready pod/python-with-boost pod/python-no-boost

python_with_boost_start_time=$(kubectl get pods python-with-boost -o go-template='{{ range .status.containerStatuses }}{{ if eq .name "python" }}{{ .state.running.startedAt }}{{ end }}{{ end }}')
python_with_boost_ready_time=$(kubectl get pods python-with-boost -o go-template='{{ range .status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ .lastTransitionTime }}{{ end }}{{ end }}')
python_with_boost_seconds_to_be_ready=$(( $( date -d "$python_with_boost_ready_time" +%s ) - $( date -d "$python_with_boost_start_time" +%s ) ))
echo "[INFO] python-with-boost pod took $python_with_boost_seconds_to_be_ready second(s) to be ready"

python_no_boost_start_time=$(kubectl get pods python-no-boost -o go-template='{{ range .status.containerStatuses }}{{ if eq .name "python" }}{{ .state.running.startedAt }}{{ end }}{{ end }}')
python_no_boost_ready_time=$(kubectl get pods python-no-boost -o go-template='{{ range .status.conditions }}{{ if (and (eq .type "Ready") (eq .status "True")) }}{{ .lastTransitionTime }}{{ end }}{{ end }}')
python_no_boost_seconds_to_be_ready=$(( $( date -d "$python_no_boost_ready_time" +%s ) - $( date -d "$python_no_boost_start_time" +%s ) ))
echo "[INFO] python-no-boost pod took $python_no_boost_seconds_to_be_ready second(s) to be ready"

# python-with-boost should start <ready_time_minimum_ratio> times quicker than python-no-boost
ready_time_minimum_ratio=5

if [ $(( $python_no_boost_seconds_to_be_ready / $python_with_boost_seconds_to_be_ready )) -ge $ready_time_minimum_ratio ]
then
    echo -e "\033[0;32m[SUCCESS]\033[0m python-with-boost started more than $ready_time_minimum_ratio times quicker than python-no-boost"
else
    echo -e "\033[0;31m[FAILURE]\033[0m python-with-boost should start more than $ready_time_minimum_ratio times quicker than python-no-boost"
    exit 1
fi

kubectl delete \
    -f ./test/e2e/python-no-boost.yaml \
    -f ./test/e2e/python-with-boost.yaml
