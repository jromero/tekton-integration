#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set +x

function wait_for_port() {
  timeout 15 bash -c "while ! nc -z localhost $1 </dev/null; do sleep 1; done"
}

# start and configure k8s
kind delete cluster --name="test" || true
kind create cluster --name="test" # --config kind-config.yml
export KUBECONFIG="$(kind get kubeconfig-path --name="test")"
kubectl cluster-info

# install tekton
kubectl apply --filename https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml
sleep 20
kubectl --namespace tekton-pipelines wait --for=condition=Ready --timeout=20s pods --all

# install buildpacks task
kubectl apply -f https://raw.githubusercontent.com/tektoncd/catalog/master/buildpacks/buildpacks-v3.yaml

# start a registry
hohup docker run -d -p 5000:5000 registry:2 &> registry.log & 
wait_for_port 5000

# setup build
mkdir out/ || true
ip_address=$(ifconfig | sed -En 's/127.0.0.1//;s/.*inet (addr:)?(([0-9]*\.){3}[0-9]*).*/\2/p' | head -1)
sed "s/IP_ADDRESS/${ip_address}/g" build.tmpl.yml > out/build.yml

# start build
kubectl apply -f out/build.yml
sleep 60
#kubectl wait --for=condition=Completed --timeout=60s pods -l tekton.dev/taskRun="test-run"

# run image
docker pull localhost:50397/my-repo/my-image

# verify http request
docker run -it -p 8080:8080 localhost:5000/my-repo/my-image  &> app.log &
wait_for_port 8080
curl http://localhost:8080/