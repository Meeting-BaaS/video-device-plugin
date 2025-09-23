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
    linux-modules-extra-6.8.0-64-generic \
    linux-headers-6.8.0-64-generic \
    v4l2loopback-dkms \
    v4l2loopback-utils \
    v4l-utils \
    zstd \
    ca-certificates \
    dkms \
    build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean


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
