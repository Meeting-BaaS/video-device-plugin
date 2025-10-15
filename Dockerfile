# Multi-stage Dockerfile for Video Device Plugin
# Stage 1: Build Go application
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

# Stage 2: Runtime environment
FROM ubuntu:24.04

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC

# Install runtime dependencies
RUN apt-get update && \
    apt-get upgrade --yes && \
    apt-get --allow-downgrades --no-install-recommends --yes install \
    kmod \
    linux-modules-extra-6.8.0-85-generic \
    linux-headers-6.8.0-85-generic \
    v4l-utils \
    zstd \
    ca-certificates \
    dkms \
    build-essential \
    git \
    wget \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

# Install latest v4l2loopback from source (version 0.15.1)
RUN cd /tmp && \
    wget https://github.com/umlaeute/v4l2loopback/archive/refs/tags/v0.15.1.tar.gz && \
    tar -xzf v0.15.1.tar.gz && \
    cd v4l2loopback-0.15.1 && \
    make all && \
    make install && \
    make install-utils && \
    # Remove old module to ensure new version is loaded (hardcoded kernel version for build env)
    rm -f /lib/modules/6.8.0-85-generic/kernel/v4l2loopback/v4l2loopback.ko.zst && \
    cd / && \
    rm -rf /tmp/v4l2loopback-0.15.1 /tmp/v0.15.1.tar.gz


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
