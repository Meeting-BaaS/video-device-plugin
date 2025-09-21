#!/bin/bash

# Video Device Plugin Startup Script
# This script initializes v4l2loopback and starts the device plugin

set -e

echo "🚀 Starting Video Device Plugin Container"
echo "=========================================="

# Function to log with timestamp
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# Function to check if running as root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        log "❌ This container must run as root to access kernel modules"
        exit 1
    fi
    log "✅ Running as root"
}

# Function to load v4l2loopback module
load_v4l2loopback() {
    log "📦 Loading v4l2loopback kernel module..."
    
    # Check if module is already loaded
    if lsmod | grep -q v4l2loopback; then
        log "✅ v4l2loopback module already loaded"
        return 0
    fi
    
    # Load the module with our specific parameters
    modprobe v4l2loopback \
        devices=8 \
        max_buffers=2 \
        exclusive_caps=1 \
        card_label="MeetingBot_WebCam"
    
    if [ $? -eq 0 ]; then
        log "✅ v4l2loopback module loaded successfully"
    else
        log "❌ Failed to load v4l2loopback module"
        exit 1
    fi
}

# Function to verify devices were created
verify_devices() {
    log "🔍 Verifying video devices..."
    
    local device_count=0
    local max_devices=${MAX_DEVICES:-8}
    
    # Wait for devices to be created (up to 30 seconds)
    local timeout=30
    local elapsed=0
    
    while [ $elapsed -lt $timeout ]; do
        device_count=$(ls /dev/video* 2>/dev/null | wc -l)
        if [ $device_count -ge $max_devices ]; then
            break
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    
    if [ $device_count -lt $max_devices ]; then
        log "❌ Expected $max_devices devices, but only found $device_count"
        log "Available devices: $(ls /dev/video* 2>/dev/null || echo 'none')"
        exit 1
    fi
    
    log "✅ Found $device_count video devices:"
    ls -la /dev/video* | while read line; do
        log "   $line"
    done
}

# Function to set device permissions
set_device_permissions() {
    log "🔐 Setting device permissions..."
    
    # Make devices readable and writable
    chmod 666 /dev/video* 2>/dev/null || true
    
    log "✅ Device permissions set"
}

# Function to display system information
display_system_info() {
    log "📊 System Information:"
    log "   Kernel version: $(uname -r)"
    log "   Architecture: $(uname -m)"
    log "   Available memory: $(free -h | grep '^Mem:' | awk '{print $2}')"
    log "   Loaded modules: $(lsmod | grep v4l2 | wc -l) v4l2 modules"
}

# Function to start the device plugin
start_device_plugin() {
    log "🎯 Starting video device plugin..."
    
    # Set environment variables if not already set
    export NODE_NAME=${NODE_NAME:-"unknown-node"}
    export MAX_DEVICES=${MAX_DEVICES:-8}
    export LOG_LEVEL=${LOG_LEVEL:-"info"}
    export RESOURCE_NAME=${RESOURCE_NAME:-"meeting-baas.io/video-devices"}
    export SOCKET_PATH=${SOCKET_PATH:-"/var/lib/kubelet/device-plugins/video-device-plugin.sock"}
    
    log "Configuration:"
    log "   NODE_NAME: $NODE_NAME"
    log "   MAX_DEVICES: $MAX_DEVICES"
    log "   LOG_LEVEL: $LOG_LEVEL"
    log "   RESOURCE_NAME: $RESOURCE_NAME"
    log "   SOCKET_PATH: $SOCKET_PATH"
    
    # Start the device plugin
    exec /usr/local/bin/video-device-plugin
}

# Function to handle cleanup on exit
cleanup() {
    log "🧹 Cleaning up on exit..."
    # Add any cleanup logic here if needed
}

# Set up signal handlers
trap cleanup EXIT INT TERM

# Main execution
main() {
    log "Starting Video Device Plugin initialization..."
    
    # Check if running as root
    check_root
    
    # Display system information
    display_system_info
    
    # Load v4l2loopback module
    load_v4l2loopback
    
    # Verify devices were created
    verify_devices
    
    # Set device permissions
    set_device_permissions
    
    # Start the device plugin
    start_device_plugin
}

# Run main function
main "$@"
