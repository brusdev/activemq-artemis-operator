#!/bin/sh

source ./scripts/go-mod-env.sh

REGISTRY=quay.io/artemiscloud
IMAGE=activemq-artemis-operator
TAG=0.17.0
CFLAGS="--redhat --build-tech-preview"

./scripts/go-gen.sh

if [[ -z ${CI} ]]; then
    ./scripts/go-test.sh
    operator-sdk build ${REGISTRY}/${IMAGE}:${TAG}
   
else
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -a -o build/_output/bin/activemq-artemis-operator github.com/artemiscloud/activemq-artemis-operator/cmd/manager
fi



