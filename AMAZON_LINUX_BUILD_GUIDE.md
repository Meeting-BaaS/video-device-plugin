# Building for Amazon Linux 2023 / Amazon EKS

This guide explains how to build the video device plugin for Amazon Linux 2023 nodes running on Amazon EKS clusters.

## Overview

The default Dockerfile is configured for Ubuntu 24.04. To build for Amazon Linux 2023 (used by Amazon EKS), you'll need to modify the Dockerfile to use Amazon Linux packages and kernel structure.

## Environment Differences

| Aspect                     | Ubuntu 24.04 (Current)          | Amazon Linux 2023 (Target)                        |
| -------------------------- | ------------------------------- | ------------------------------------------------- |
| **Base Image**             | `ubuntu:24.04`                  | `amazonlinux:2023`                                |
| **Package Manager**        | `apt-get`                       | `yum` / `dnf`                                     |
| **Kernel Version**         | `6.8.0-85-generic`              | `6.1.141-155.222.amzn2023.x86_64`                 |
| **Kernel Headers Package** | `linux-headers-<version>`       | `kernel-devel-<version>`                          |
| **Kernel Modules Package** | `linux-modules-extra-<version>` | `kernel-modules-extra` (if available) or built-in |
| **Build Tools**            | `build-essential`               | `gcc make kernel-devel`                           |
| **v4l-utils**              | Available via apt               | May need EPEL or build from source                |

## Required Dockerfile Changes

### 1. Base Image Change

```dockerfile
# Current
FROM ubuntu:24.04

# Amazon Linux 2023
FROM amazonlinux:2023
```

### 2. Package Manager Commands

```dockerfile
# Current (Ubuntu)
RUN apt-get update && \
    apt-get upgrade --yes && \
    apt-get --allow-downgrades --no-install-recommends --yes install \
    kmod \
    linux-modules-extra-${KERNEL_VERSION} \
    linux-headers-${KERNEL_VERSION} \
    ...

# Amazon Linux 2023
RUN yum update -y && \
    yum install -y \
    kmod \
    kernel-devel-${KERNEL_VERSION} \
    kernel-modules-extra \
    gcc \
    make \
    ...
```

### 3. Kernel Version Format

```dockerfile
# Current
ARG KERNEL_VERSION=6.8.0-85-generic

# Amazon Linux 2023
ARG KERNEL_VERSION=6.1.141-155.222.amzn2023.x86_64
```

### 4. Package Name Differences

| Ubuntu Package              | Amazon Linux Equivalent                   |
| --------------------------- | ----------------------------------------- |
| `linux-headers-<ver>`       | `kernel-devel-<ver>`                      |
| `linux-modules-extra-<ver>` | `kernel-modules-extra` (or may not exist) |
| `build-essential`           | `gcc make` (individual packages)          |
| `v4l-utils`                 | May need EPEL or build from source        |
| `wget`                      | `wget` (same)                             |
| `ca-certificates`           | `ca-certificates` (same)                  |

### 5. Module Path Differences

Amazon Linux may use different module paths:

- `/lib/modules/<version>/build` (same)
- `/lib/modules/<version>/extra` (may differ)
- Module installation path might be different

## Complete Dockerfile for Amazon Linux 2023

```dockerfile
# Multi-stage Dockerfile for Video Device Plugin
# Stage 1: Build Go application (unchanged)
FROM golang:1.25-alpine AS go-builder

# Install git and ca-certificates for go mod download
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the Go application
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -a -installsuffix cgo \
    -ldflags '-w -s' \
    -o video-device-plugin .

# Stage 2: Runtime environment - Amazon Linux 2023
FROM amazonlinux:2023

# Set environment variables
ENV TZ=UTC

# Install runtime dependencies
# Build the module in-image for the target kernel version (parameterized via ARG)
ARG KERNEL_VERSION=6.1.141-155.222.amzn2023.x86_64
RUN yum update -y && \
    yum install -y \
    kmod \
    kernel-devel-${KERNEL_VERSION} \
    gcc \
    make \
    wget \
    ca-certificates \
    && yum clean all

# Note: v4l-utils may not be available in Amazon Linux base repos
# You may need to:
# 1. Enable EPEL: yum install -y epel-release && yum install -y v4l-utils
# 2. Or build from source
# 3. Or skip if not needed for runtime (only needed for v4l2-ctl utility)

# Install latest v4l2loopback from source (version 0.15.1)
# Build the module against the target kernel version specified in KERNEL_VERSION
WORKDIR /tmp
RUN wget -nv https://github.com/umlaeute/v4l2loopback/archive/refs/tags/v0.15.1.tar.gz && \
    tar -xzf v0.15.1.tar.gz && \
    cd v4l2loopback-0.15.1 && \
    # Build and install userland utility (v4l2loopback-ctl)
    make install-utils && \
    # Build the kernel module against the target kernel version
    make all KERNELDIR=/lib/modules/${KERNEL_VERSION}/build && \
    make install && \
    # Remove any older module variants for this kernel (robust cleanup)
    find /lib/modules/${KERNEL_VERSION} -type f -name 'v4l2loopback.ko*' ! -path "*/updates/v4l2loopback.ko" -delete || true && \
    cd / && \
    rm -rf /tmp/v4l2loopback-0.15.1 /tmp/v0.15.1.tar.gz

# Remove build-only tools after installation
RUN yum remove -y wget gcc make && \
    yum clean all

# Note: The module is built for the kernel version specified in KERNEL_VERSION ARG.
# Ensure all nodes in your cluster run this kernel version, or rebuild the image
# with a different KERNEL_VERSION ARG for different kernel versions.

# Copy Go binary from builder stage
COPY --from=go-builder /app/video-device-plugin /usr/local/bin/video-device-plugin

# Create necessary directories
RUN mkdir -p /var/lib/kubelet/device-plugins

# Set working directory
WORKDIR /usr/local/bin

# Create non-root user for security (optional)
RUN groupadd -r videoplugin && useradd -r -g videoplugin videoplugin

# Expose metrics port (if enabled)
EXPOSE 8080

# Set the Go binary as the entrypoint
ENTRYPOINT ["/usr/local/bin/video-device-plugin"]
```

## Key Changes Summary

### 1. Base Image

- ✅ Change from `ubuntu:24.04` to `amazonlinux:2023`

### 2. Package Manager

- ✅ Replace all `apt-get` commands with `yum`
- ✅ Remove `DEBIAN_FRONTEND=noninteractive` (not needed for yum)
- ✅ Use `yum clean all` instead of `apt-get clean`

### 3. Package Names

- ✅ `linux-headers-*` → `kernel-devel-*`
- ✅ `linux-modules-extra-*` → May not exist, check availability
- ✅ `build-essential` → `gcc make` (individual packages)
- ✅ `v4l-utils` → May need EPEL or build from source

### 4. Kernel Version

- ✅ Update default `KERNEL_VERSION` to `6.1.141-155.222.amzn2023.x86_64`
- ✅ Update in `.env.example` and `docker-build.sh` default

### 5. Module Path Discovery

The code in `module_manager.go` should work as-is since it searches multiple paths, but verify:

- `/lib/modules/<version>/updates/v4l2loopback.ko` (should work)
- `/lib/modules/<version>/extra/v4l2loopback.ko` (may differ)
- `/lib/modules/<version>/kernel/drivers/media/v4l2loopback.ko` (should work)

## Additional Considerations

### 1. v4l-utils Availability

Amazon Linux 2023 may not have `v4l-utils` in base repos. Options:

- **Option A**: Enable EPEL and install

  ```dockerfile
  RUN yum install -y epel-release && \
      yum install -y v4l-utils
  ```

- **Option B**: Build from source (if needed)
- **Option C**: Skip if not needed (v4l2loopback-ctl is built from source)

### 2. Kernel Modules Extra

Amazon Linux may not have a separate `kernel-modules-extra` package. The modules might be:

- Built into the kernel
- Available in `kernel-devel` package
- Not needed if v4l2loopback is built from source

### 3. Testing Required

- Verify kernel headers package name and availability
- Test module compilation
- Verify module path after installation
- Test module loading at runtime

### 4. Build Script Updates

Update `docker-build.sh` default:

```bash
KERNEL_VERSION="${KERNEL_VERSION:-6.1.141-155.222.amzn2023.x86_64}"
```

### 5. .env.example Updates

Update default kernel version in `.env.example`:

```bash
KERNEL_VERSION=6.1.141-155.222.amzn2023.x86_64
```

## Building for Amazon Linux 2023

1. **Create Amazon Linux Dockerfile**

   - Copy current Dockerfile
   - Apply all changes above
   - Test build locally

2. **Verify Package Availability**

   ```bash
   docker run -it amazonlinux:2023 bash
   yum search kernel-devel
   yum search v4l-utils
   ```

3. **Test Module Compilation**

   - Build image with Amazon Linux base
   - Verify module compiles successfully
   - Check module is in expected location

4. **Update Configuration**

   - Update `.env.example` with Amazon Linux kernel version
   - Update `docker-build.sh` default
   - Update README with Amazon Linux instructions

5. **Test in EKS**
   - Deploy to test EKS cluster
   - Verify module loads correctly
   - Test device allocation

## Potential Issues

1. **Kernel Version Mismatch**: EKS nodes might have slightly different kernel versions

   - Solution: Use kernel version from actual node: `uname -r` on node
   - Consider building multiple images for different kernel versions

2. **Package Availability**: Some packages might not be available

   - Solution: Build from source or use alternative repos (EPEL)

3. **Module Path Differences**: Module installation path might differ

   - Solution: The path discovery code should handle this, but verify

4. **SELinux**: Amazon Linux may have SELinux enabled
   - Solution: Ensure container has proper SELinux context or run in permissive mode

## Recommended Approach

1. **Create separate Dockerfile** (e.g., `Dockerfile.amazonlinux`) for Amazon Linux builds
2. **Test thoroughly** in a development EKS cluster before production
3. **Verify kernel version** matches your EKS nodes exactly
4. **Document kernel version** in your build configuration

## Quick Start

For Amazon EKS with kernel `6.1.141-155.222.amzn2023.x86_64`:

1. Use the Dockerfile example provided in this guide
2. Build with: `docker build --build-arg KERNEL_VERSION=6.1.141-155.222.amzn2023.x86_64 -t video-device-plugin:amazonlinux .`
3. Deploy to your EKS cluster following the standard deployment instructions in the README
