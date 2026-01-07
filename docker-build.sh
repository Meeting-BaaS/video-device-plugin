#!/bin/bash

# Simple Docker Build Script for Video Device Plugin
set -e

# Load environment variables from .env if it exists
if [ -f ".env" ]; then
    echo "Loading environment variables from .env..."
    set -a
    source .env
    set +a
fi

# Check required environment variables
if [ -z "$DOCKER_REGISTRY" ] || [ -z "$DOCKER_USERNAME" ] || [ -z "$DOCKER_PASSWORD" ]; then
    echo "Error: Missing required Docker registry environment variables"
    echo "Please set DOCKER_REGISTRY, DOCKER_USERNAME, and DOCKER_PASSWORD"
    echo "You can either:"
    echo "  1. Set them as environment variables"
    echo "  2. Create a .env file with these variables"
    echo "  3. Copy .env.example to .env and edit it"
    exit 1
fi

# Configuration
IMAGE_NAME="video-device-plugin"
IMAGE_TAG="1.0.0"
FULL_IMAGE_NAME="${DOCKER_REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"

# Kernel version for module compilation (default if not set)
KERNEL_VERSION="${KERNEL_VERSION:-6.8.0-90-generic}"

echo "Building Docker image: $FULL_IMAGE_NAME"
echo "Using kernel version: $KERNEL_VERSION"

# Build the image with kernel version build arg
docker build --build-arg KERNEL_VERSION="$KERNEL_VERSION" --tag "$FULL_IMAGE_NAME" .

echo "Docker image built successfully"

# Login to registry
echo "Logging in to Docker registry..."
echo "$DOCKER_PASSWORD" | docker login "$DOCKER_REGISTRY" --username "$DOCKER_USERNAME" --password-stdin

# Push the image
echo "Pushing image to registry..."
docker push "$FULL_IMAGE_NAME"

echo "Image pushed successfully: $FULL_IMAGE_NAME"

# Show image info
echo "Image information:"
docker image inspect "$FULL_IMAGE_NAME" --format="  Name: {{.RepoTags}}
  Size: {{.Size}}
  Created: {{.Created}}
  Architecture: {{.Architecture}}
  OS: {{.Os}}"

echo ""
echo "Build and push completed successfully!"
echo "Image: $FULL_IMAGE_NAME"
