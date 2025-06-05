#!/bin/bash

# Exit on error
set -e

# Default values
IMAGE_NAME="open_agent"
IMAGE_TAG="latest"
REGISTRY="whatap"
PUSH=true
ARCH="all"  # Default to building for all architectures
VERSION=""   # Default empty version (will be set to IMAGE_TAG if not specified)
COMMIT_HASH="" # Default empty commit hash

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --tag|-t)
      IMAGE_TAG="$2"
      shift 2
      ;;
    --registry|-r)
      REGISTRY="$2"
      shift 2
      ;;
    --push|-p)
      PUSH=true
      shift
      ;;
    --arch|-a)
      ARCH="$2"
      shift 2
      ;;
    --version|-v)
      VERSION="$2"
      shift 2
      ;;
    --commit|-c)
      COMMIT_HASH="$2"
      shift 2
      ;;
    --help|-h)
      echo "Usage: $0 [options]"
      echo "Options:"
      echo "  --tag, -t TAG       Set the image tag (default: latest)"
      echo "  --registry, -r REG  Set the registry (e.g., docker.io/username)"
      echo "  --push, -p          Push the image to the registry"
      echo "  --arch, -a ARCH     Set the target architecture: amd64, arm64, or all (default: all)"
      echo "  --version, -v VER   Set the agent version (default: same as tag)"
      echo "  --commit, -c HASH   Set the commit hash (default: current git commit)"
      echo "  --help, -h          Show this help message"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Validate architecture
if [[ "$ARCH" != "amd64" && "$ARCH" != "arm64" && "$ARCH" != "all" ]]; then
  echo "Error: Invalid architecture. Must be amd64, arm64, or all."
  exit 1
fi

# Set default values for VERSION and COMMIT_HASH if not specified
if [ -z "$VERSION" ]; then
  VERSION="$IMAGE_TAG"
  echo "Using image tag as version: $VERSION"
fi

if [ -z "$COMMIT_HASH" ]; then
  COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
  echo "Using current git commit hash: $COMMIT_HASH"
fi

# Set the full image name
if [ -n "$REGISTRY" ]; then
  FULL_IMAGE_NAME="$REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
else
  FULL_IMAGE_NAME="$IMAGE_NAME:$IMAGE_TAG"
fi

# Check if Docker Buildx is available
if ! docker buildx version > /dev/null 2>&1; then
  echo "Error: Docker Buildx is required for multi-architecture builds."
  echo "Please install Docker Buildx or upgrade your Docker installation."
  exit 1
fi

# Ensure we have a builder instance
BUILDER_NAME="openagent-builder"
if ! docker buildx inspect "$BUILDER_NAME" > /dev/null 2>&1; then
  echo "Creating Docker Buildx builder instance: $BUILDER_NAME"
  docker buildx create --name "$BUILDER_NAME" --use
else
  docker buildx use "$BUILDER_NAME"
fi

# Build the image
echo "Building Docker image: $FULL_IMAGE_NAME for architecture: $ARCH"

# Set build platform based on architecture
if [ "$ARCH" = "all" ]; then
  PLATFORMS="linux/amd64,linux/arm64"
elif [ "$ARCH" = "amd64" ]; then
  PLATFORMS="linux/amd64"
else
  PLATFORMS="linux/arm64"
fi

# Build and optionally push the image
BUILD_ARGS="--platform $PLATFORMS"
if [ "$PUSH" = true ]; then
  if [ -z "$REGISTRY" ]; then
    echo "Error: Registry must be specified when pushing the image"
    exit 1
  fi

  echo "Building and pushing Docker image: $FULL_IMAGE_NAME"
  BUILD_ARGS="$BUILD_ARGS --push"
else
  # If not pushing, load the image into Docker (only works for single platform)
  if [ "$ARCH" != "all" ]; then
    BUILD_ARGS="$BUILD_ARGS --load"
  else
    echo "Warning: Building for multiple platforms without pushing."
    echo "The resulting image will not be loaded into Docker."
  fi
fi

# Execute the build
docker buildx build $BUILD_ARGS --build-arg VERSION="$VERSION" --build-arg COMMIT_HASH="$COMMIT_HASH" -t "$FULL_IMAGE_NAME" .

echo "Done!"
