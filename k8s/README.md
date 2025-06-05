# OpenAgent Kubernetes Deployment Guide

This guide provides instructions for deploying the OpenAgent to a Kubernetes cluster.

## Prerequisites

- Docker installed on your local machine
- Access to a Kubernetes cluster
- `kubectl` command-line tool configured to communicate with your cluster
- WHATAP account with license key, host, and port information

## Building the Docker Image

1. Clone the repository:
   ```bash
   git clone <repository-url>
   cd openagent
   ```

2. Build the Docker image:
   ```bash
   docker build -t openagent:latest .
   ```

3. (Optional) Push the image to a container registry:
   ```bash
   docker tag openagent:latest <registry>/<username>/openagent:latest
   docker push <registry>/<username>/openagent:latest
   ```

   If you push to a private registry, you'll need to update the image reference in the deployment.yaml file and create a Kubernetes secret for pulling the image.

## Configuring the Deployment

### Option 1: Using the create-whatap-secret.sh script (Recommended)

1. Use the provided script to create the secret:
   ```bash
   ./k8s/create-whatap-secret.sh <WHATAP_LICENSE> <WHATAP_HOST> <WHATAP_PORT>
   ```

   Example:
   ```bash
   ./k8s/create-whatap-secret.sh x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502 15.165.146.117 6600
   ```

   This script creates a Kubernetes secret named `whatap-credentials` with the provided values.

### Option 2: Manual Secret Creation

1. Create the secret directly using kubectl:
   ```bash
   kubectl create secret generic whatap-credentials \
       --from-literal=license=<WHATAP_LICENSE> \
       --from-literal=host=<WHATAP_HOST> \
       --from-literal=port=<WHATAP_PORT>
   ```

(Optional) Customize the ConfigMap in `deployment.yaml` to adjust the scraping configuration.

## Deploying to Kubernetes

### Option 1: Using the deploy-openagent.sh script (Recommended)

The easiest way to deploy OpenAgent is to use the provided deploy-openagent.sh script:

```bash
./k8s/deploy-openagent.sh <WHATAP_LICENSE> <WHATAP_HOST> <WHATAP_PORT>
```

Example:
```bash
./k8s/deploy-openagent.sh x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502 15.165.146.117 6600
```

This script will:
1. Create the Whatap credentials secret
2. Deploy the OpenAgent
3. Wait for the deployment to be ready

### Option 2: Manual Deployment

If you prefer to deploy manually, follow these steps:

1. Create the Whatap credentials secret (see "Configuring the Deployment" section above)

2. Apply the Kubernetes manifests:
   ```bash
   kubectl apply -f k8s/deployment.yaml
   ```

3. Verify the deployment:
   ```bash
   kubectl get pods -l app=openagent
   ```

4. Check the logs:
   ```bash
   kubectl logs -l app=openagent
   ```

## Customizing the Configuration

The OpenAgent uses a configuration file (`scrape_config.yaml`) to determine what metrics to scrape. This configuration is stored in a ConfigMap and mounted into the container.

To update the configuration:

1. Edit the ConfigMap in `deployment.yaml`
2. Apply the changes:
   ```bash
   kubectl apply -f k8s/deployment.yaml
   ```
3. Restart the deployment to pick up the changes:
   ```bash
   kubectl rollout restart deployment openagent
   ```

## Troubleshooting

If you encounter issues with the deployment, check the following:

1. Pod status:
   ```bash
   kubectl describe pod -l app=openagent
   ```

2. Container logs:
   ```bash
   kubectl logs -l app=openagent
   ```

3. Check if the ServiceAccount has the correct permissions:
   ```bash
   kubectl auth can-i get pods --as=system:serviceaccount:default:openagent-sa
   kubectl auth can-i list services --as=system:serviceaccount:default:openagent-sa
   kubectl auth can-i watch endpoints --as=system:serviceaccount:default:openagent-sa
   ```

4. Verify the Secret exists and contains the correct values:
   ```bash
   kubectl get secret whatap-credentials -o yaml
   ```

## Uninstalling

To remove the OpenAgent from your cluster:

```bash
kubectl delete -f k8s/deployment.yaml
```
