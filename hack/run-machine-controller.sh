#!/usr/bin/env bash

set -e

DNS_IP=${DNS_IP:-172.16.0.10}

make -C $(dirname $0)/.. machine-controller
$(dirname $0)/../machine-controller \
  -kubeconfig=$(dirname $0)/../.kubeconfig \
  -worker-count=50 \
  -logtostderr \
  -v=6 \
  -cluster-dns=$DNS_IP \
  -internal-listen-address=0.0.0.0:8085
