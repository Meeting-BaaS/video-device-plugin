# Video Device Plugin Implementation Guide

## Repository Name

**`video-device-plugin`** - Clear, descriptive, and follows Kubernetes naming conventions.

## Project Overview

A Kubernetes device plugin that manages v4l2loopback virtual camera devices for meeting bots. The plugin runs as a DaemonSet, pre-installs v4l2loopback kernel module, creates video devices, and provides device allocation to pods.

## Repository Structure

```text
video-device-plugin/
├── main.go                     # Main application entry point
├── device_plugin.go            # Device plugin gRPC server
├── v4l2_manager.go             # V4L2 device management
├── types.go                    # Go type definitions
├── utils.go                    # Utility functions
├── go.mod                      # Go module dependencies
├── go.sum                      # Go module checksums
├── scripts/
│   ├── build.sh                # Build script
│   └── test.sh                 # Test script
├── .github/
│   └── workflows/
│       ├── build.yml           # CI/CD build pipeline
│       └── release.yml         # Release pipeline
├── Dockerfile                  # Multi-stage Docker build
├── start.sh                    # Container startup script
├── .gitignore                  # Git ignore rules
├── README.md                   # Project documentation
└── CHANGELOG.md                # Version history
```

## Core Implementation Requirements

### 1. Multi-Stage Dockerfile

- **Stage 1**: Build v4l2loopback kernel module from source
- **Stage 2**: Build Go application
- **Stage 3**: Runtime environment with pre-installed v4l2loopback
- **Optimization**: Single binary, no runtime dependencies, minimal image size

### 2. Go Application Structure

- **main.go**: Application entry point with graceful shutdown handling
- **device_plugin.go**: gRPC server implementing Kubernetes device plugin interface
- **v4l2_manager.go**: V4L2 device management and kernel module handling
- **types.go**: Go structs and interfaces for device plugin communication
- **utils.go**: Utility functions for device operations

### 3. Go Module Dependencies

- **go.mod**: Go module definition with Kubernetes and gRPC dependencies
- **Dependencies**: k8s.io/kubelet, google.golang.org/grpc, standard library
- **No External Runtime**: Single binary with no runtime dependencies

### 4. Device Plugin Interface Implementation

- **ListAndWatch**: Stream device availability to kubelet
- **Allocate**: Assign specific devices to pods
- **GetDevicePluginOptions**: Return plugin configuration
- **PreStartContainer**: Optional container pre-start logic

### 5. V4L2 Device Management

- **Module Loading**: Check and load v4l2loopback kernel module
- **Device Creation**: Create /dev/videoX devices (0-7 by default)
- **Device Allocation**: Track device usage and prevent conflicts
- **Health Monitoring**: Monitor device health and availability

### 6. Startup Script (start.sh)

- **Module Check**: Verify v4l2loopback is loaded
- **Device Creation**: Create video devices if they don't exist
- **Application Start**: Launch the Go device plugin binary

## Key Features

### Performance Optimizations

- **Pre-installed v4l2loopback**: Faster startup times for autoscaling
- **Efficient Device Management**: Quick device allocation/deallocation
- **Health Checks**: Built-in monitoring for device availability
- **Graceful Shutdown**: Proper cleanup on pod termination

### Scalability Features

- **Configurable Device Count**: Environment variable for max devices per node
- **Resource Management**: Proper device accounting and limits
- **Node Affinity**: Works with Kubernetes node scheduling
- **Auto-scaling Support**: Optimized for dynamic node scaling

### Security Features

- **Privileged Container**: Required for kernel module access
- **Device Isolation**: Each pod gets exclusive device access
- **RBAC Support**: Proper Kubernetes role-based access control
- **Resource Limits**: Prevent device over-allocation

## Configuration

### Environment Variables

- **MAX_DEVICES**: Maximum number of video devices per node (default: 8)
- **NODE_NAME**: Kubernetes node name for device plugin registration
- **KUBELET_SOCKET**: Path to kubelet device plugin socket

### Device Plugin Configuration

- **Resource Name**: `meeting-baas.io/video-devices`
- **Socket Path**: `/var/lib/kubelet/device-plugins/video-device-plugin.sock`
- **Health Check**: Monitor device availability and module status

## CI/CD Pipeline

### Build Pipeline

- **Go Setup**: Install Go dependencies and build binary
- **Docker Build**: Multi-stage build with v4l2loopback
- **Image Push**: Push to Scaleway Container Registry
- **Testing**: Run unit tests and linting

### Release Pipeline

- **Tag-based Releases**: Automatic releases on version tags
- **Image Tagging**: Tag images with version numbers
- **Registry Push**: Push release images to production registry

## Integration with Current Setup

### Helm Chart Updates Required

- **New Chart**: `video_device_manager_chart` for device plugin DaemonSet
- **Updated Chart**: `meeting_bots_chart` to request video devices
- **Resource Requests**: Add `meeting-baas.io/video-devices` resource requests

### Deployment Strategy

1. **Preprod Testing**: Deploy device plugin to preprod environment
2. **Bot Integration**: Update meeting bots to use video devices
3. **Production Rollout**: Deploy to production with monitoring
4. **Monitoring**: Set up alerts for device plugin health

## Testing Strategy

### Unit Tests

- **V4L2Manager**: Test device creation and allocation logic
- **DevicePlugin**: Test gRPC interface implementation
- **Utils**: Test utility functions and error handling
- **Go Testing**: Use built-in `go test` framework

### Integration Tests

- **Docker Build**: Test multi-stage build process
- **Module Loading**: Test v4l2loopback module installation
- **Device Creation**: Test video device creation and access
- **Binary Testing**: Test Go binary execution

### End-to-End Tests

- **Kubernetes Deployment**: Test DaemonSet deployment
- **Device Allocation**: Test pod device allocation
- **Meeting Bot Integration**: Test with actual meeting bot pods

## Monitoring and Observability

### Logging

- **Structured Logging**: JSON-formatted logs for better parsing
- **Log Levels**: Debug, Info, Warn, Error with appropriate filtering
- **Context Information**: Include pod ID, node name, device IDs in logs

### Metrics

- **Device Count**: Track available and allocated devices
- **Allocation Rate**: Monitor device allocation frequency
- **Error Rate**: Track device allocation failures
- **Startup Time**: Monitor device plugin initialization time

### Health Checks

- **Liveness Probe**: Check if device plugin is running
- **Readiness Probe**: Check if devices are available
- **Module Health**: Verify v4l2loopback module status

## Security Considerations

### Container Security

- **Privileged Mode**: Required for kernel module access
- **Host Network**: Required for device plugin communication
- **Volume Mounts**: Secure mounting of device directories
- **Single Binary**: Reduced attack surface with no runtime dependencies

### RBAC Configuration

- **Service Account**: Dedicated service account for device plugin
- **Cluster Role**: Permissions for node and pod access
- **Cluster Role Binding**: Bind service account to cluster role

### Device Security

- **Device Isolation**: Each pod gets exclusive device access
- **Permission Management**: Proper device file permissions
- **Access Control**: Prevent unauthorized device access

## Performance Considerations

### Startup Optimization

- **Pre-installed Module**: Faster module loading
- **Single Binary**: No runtime dependencies or compilation
- **Parallel Initialization**: Concurrent device creation
- **Caching**: Cache device information for faster access

### Runtime Optimization

- **Efficient Allocation**: Quick device assignment algorithm
- **Memory Management**: Minimal memory footprint with Go's garbage collector
- **CPU Usage**: Low CPU overhead for device management
- **Native Performance**: Go's compiled binary performance

### Autoscaling Optimization

- **Fast Startup**: Quick pod initialization for scaling
- **Resource Efficiency**: Minimal resource usage per node
- **Predictable Performance**: Consistent behavior across nodes
- **Small Image Size**: Faster image pulls and container startup

## Troubleshooting

### Common Issues

- **Module Loading Failures**: Check kernel compatibility and permissions
- **Device Creation Errors**: Verify device file permissions
- **gRPC Connection Issues**: Check socket permissions and paths
- **Allocation Failures**: Monitor device availability and health

### Debugging Tools

- **Logs**: Check container logs for error messages
- **Device Status**: Verify device creation and permissions
- **Module Status**: Check v4l2loopback module loading
- **Network Connectivity**: Verify gRPC communication
- **Go Debugging**: Use `go run` for local debugging
- **Binary Analysis**: Use `go tool` for binary analysis

### Recovery Procedures

- **Pod Restart**: Automatic restart on failures
- **Module Reload**: Reinitialize v4l2loopback if needed
- **Device Recreation**: Recreate devices if corrupted
- **Cleanup**: Proper cleanup on pod termination

## Next Steps

1. **Create Repository**: Set up `video-device-plugin` repository
2. **Learn Go Basics**: Spend 2-3 days learning Go fundamentals
3. **Implement Code**: Follow the structure and requirements above
4. **Set up CI/CD**: Configure GitHub Actions for build and release
5. **Test Locally**: Test with Docker and local Kubernetes
6. **Deploy to Preprod**: Test with meeting bots in preprod
7. **Production Deployment**: Deploy to production with monitoring
8. **Update Helm Charts**: Modify current charts to use device plugin
9. **Monitor and Optimize**: Track performance and make improvements

## Success Criteria

- **Fast Startup**: Device plugin starts in under 30 seconds
- **Reliable Allocation**: 99.9% successful device allocation rate
- **Autoscaling Support**: Works seamlessly with node auto-scaling
- **Meeting Bot Integration**: Bots can access video devices without issues
- **Production Ready**: Stable operation in production environment
- **Go Proficiency**: Comfortable maintaining Go codebase long-term
