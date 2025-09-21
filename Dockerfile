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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=386 go build \
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
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean


# Copy Go binary from builder stage
COPY --from=go-builder /app/video-device-plugin /usr/local/bin/video-device-plugin

# Copy startup script
COPY start.sh /usr/local/bin/start.sh
RUN chmod +x /usr/local/bin/start.sh

# Create necessary directories
RUN mkdir -p /var/lib/kubelet/device-plugins

# Set working directory
WORKDIR /usr/local/bin

# Create non-root user for security (optional)
RUN groupadd -r videoplugin && useradd -r -g videoplugin videoplugin

# Expose metrics port (if enabled)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ls /dev/video* | wc -l | grep -q 8 || exit 1

# Use startup script as entrypoint
ENTRYPOINT ["/usr/local/bin/start.sh"]

# Default command
CMD ["video-device-plugin"]
