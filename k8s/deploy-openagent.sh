#!/bin/bash

# Exit on error
set -e

# Check if all required arguments are provided
if [ "$#" -ne 3 ]; then
    echo "Usage: $0 <WHATAP_LICENSE> <WHATAP_HOST> <WHATAP_PORT>"
    echo "Example: $0 x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502 15.165.146.117 6600"
    exit 1
fi

WHATAP_LICENSE=$1
WHATAP_HOST=$2
WHATAP_PORT=$3

# Create the Whatap credentials secret
echo "Creating Whatap credentials secret..."
./k8s/create-whatap-secret.sh ${WHATAP_LICENSE} ${WHATAP_HOST} ${WHATAP_PORT}

# Apply the deployment
echo "Deploying OpenAgent..."
kubectl apply -f k8s/deployment.yaml

# Wait for the deployment to be ready
echo "Waiting for deployment to be ready..."
kubectl rollout status deployment/openagent

echo "OpenAgent deployment complete!"
echo "You can check the status with: kubectl get pods -l app=openagent"
echo "You can view the logs with: kubectl logs -l app=openagent"