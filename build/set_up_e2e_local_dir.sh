#!/bin/bash
mkdir -p cluster_config
kind get kubeconfig > cluster_config/hub

if [ ! -d "./e2e-data/testcases" ]; then
    mkdir -p e2e-data/testcases
fi

if [ ! -d "./e2e-data/expectations" ]; then
    mkdir -p e2e-data/expectations
fi

if [ ! -d "./e2e-data/stages" ]; then
    mkdir -p e2e-data/stages
fi

echo "now you can run the applifecycle-backend-e2e binary as:"
echo "======>>>>"
echo "applifecycle-backend-e2e -cfg cluster_config -data e2e-data"
