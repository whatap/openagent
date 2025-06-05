#!/bin/bash

# Check if all required arguments are provided
if [ "$#" -ne 3 ]; then
    echo "Usage: $0 <WHATAP_LICENSE> <WHATAP_HOST> <WHATAP_PORT>"
    echo "Example: $0 x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502 15.165.146.117 6600"
    exit 1
fi

WHATAP_LICENSE=$1
WHATAP_HOST=$2
WHATAP_PORT=$3

# Create the secret
kubectl create secret generic whatap-credentials \
    --from-literal=license=${WHATAP_LICENSE} \
    --from-literal=host=${WHATAP_HOST} \
    --from-literal=port=${WHATAP_PORT}

echo "Secret 'whatap-credentials' created successfully."
echo "You can now deploy the OpenAgent using:"
echo "kubectl apply -f k8s/deployment.yaml"