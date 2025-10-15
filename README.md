# Video Device Plugin for Kubernetes

A Kubernetes device plugin that manages v4l2loopback virtual video devices for containerized applications. This plugin enables pods to access virtual video devices (like `/dev/video10`, `/dev/video11`, etc.) for video recording, streaming, and processing in containerized environments.

## 🎯 Problem Statement

Running video applications in Kubernetes containers is challenging because:

- **No native video device support**: Kubernetes doesn't provide video devices by default
- **Device conflicts**: Multiple containers can't safely share the same video device
- **Resource management**: No way to track and allocate video devices across pods
- **Scaling issues**: Video devices don't scale with pod autoscaling
- **Complex setup**: Manual v4l2loopback configuration is error-prone and not container-friendly

This plugin solves these problems by providing a **Kubernetes-native way to manage virtual video devices** with proper resource allocation, conflict prevention, and automatic scaling.

## ✨ Key Benefits

- **🎯 Kubernetes-Native**: Follows official device plugin best practices (like GPU plugins)
- **🔧 Simple & Reliable**: No complex tracking or reconciliation needed
- **⚡ High Performance**: Minimal overhead with thread-safe operations
- **🛡️ Production Ready**: Handles edge cases and provides comprehensive logging
- **📈 Scalable**: Works seamlessly with Kubernetes autoscaling
- **🔄 Robust**: Auto-recovery from kubelet restarts and real-time health updates
- **🔍 Observable**: Structured logging and per-device health monitoring
- **🚫 No Device Conflicts**: Automatic device isolation between concurrent pods
- **🆘 Fallback Mode**: Graceful degradation with dummy devices when kernel modules fail
- **🔧 Dynamic Device Management**: Automatic device reset between allocations using v4l2loopback-ctl
- **⚡ Fresh Device State**: Every pod gets a clean, reset device to prevent unresponsive states

## 🏗️ Architecture Overview

### Core Components

```text
┌─────────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                          │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────┐ │
│  │   Node 1        │    │   Node 2        │    │   Node N    │ │
│  │                 │    │                 │    │             │ │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────┐ │ │
│  │ │ DaemonSet   │ │    │ │ DaemonSet   │ │    │ │DaemonSet│ │ │
│  │ │ Pod         │ │    │ │ Pod         │ │    │ │Pod      │ │ │
│  │ │             │ │    │ │             │ │    │ │         │ │ │
│  │ │ ┌─────────┐ │ │    │ │ ┌─────────┐ │ │    │ │ ┌─────┐ │ │ │
│  │ │ │v4l2 mgr │ │ │    │ │ │v4l2 mgr │ │ │    │ │ │v4l2 │ │ │ │
│  │ │ │8 devices│ │ │    │ │ │8 devices│ │ │    │ │ │mgr  │ │ │ │
│  │ │ └─────────┘ │ │    │ │ └─────────┘ │ │    │ │ └─────┘ │ │ │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────┘ │ │
│  └─────────────────┘    └─────────────────┘    └─────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### Device Allocation Flow

```text
1. Pod requests meeting-baas.io/video-devices: 1
2. Kubelet queries device plugin for available devices via ListAndWatch
3. Device plugin reports all 8 devices as available (with health status)
4. Kubelet chooses which device to allocate (e.g., video10)
5. Kubelet calls device plugin with specific device ID
6. Device plugin validates and returns device info (env vars, mounts)
7. PreStartContainer resets the device (delete + recreate) for fresh state
8. Pod starts with VIDEO_DEVICE=/dev/video10
9. Kubernetes manages device lifecycle automatically
10. Device plugin monitors health every 30 seconds
```

## 🔧 Key Concepts

### 1. Device Plugin Architecture

The plugin implements the **Kubernetes Device Plugin API** with these key methods:

- **`ListAndWatch`**: Streams available devices to kubelet
- **`Allocate`**: Allocates specific devices to pods
- **`GetDevicePluginOptions`**: Returns plugin configuration
- **`PreStartContainer`**: Resets devices before container start for fresh state

### 2. Kubernetes-Native Device Management

The plugin follows **Kubernetes best practices** for device management:

- **Always report all devices**: ListAndWatch reports all 8 devices as available
- **Kubelet manages allocation**: Kubelet chooses which device to allocate
- **Simple validation**: Basic device existence checking
- **No allocation tracking**: Kubernetes handles device lifecycle entirely
- **Per-device health monitoring**: Individual device health status reporting

**Benefits**:

- Follows official Kubernetes device plugin patterns (like GPU plugins)
- No complex pod-to-device mapping needed
- No additional RBAC permissions required
- Reliable and maintainable
- Automatic device isolation between pods

### 3. V4L2 Device Management

**Device Range**: `/dev/video10` to `/dev/video17` (8 devices per node)

- Starts from `video10` to avoid conflicts with system devices
- Each device can be allocated to exactly one pod
- Thread-safe allocation with mutex protection
- Automatic device health monitoring

**Device Lifecycle**:

```text
Create → Available → Allocate → Use → Available
   ↓         ↓           ↓        ↓        ↓
Startup   Ready      Pod Req   Pod Run   Ready
   ↓         ↓           ↓        ↓        ↓
Health    Health      Health   Health   Health
Check     Check       Check    Check    Check
```

### 4. Health Monitoring

**Health Check Process**:

- **Per-Device Monitoring**: Each device is checked individually every 30 seconds
- **Real-time Reporting**: Kubernetes gets notified immediately when devices become unhealthy
- **Automatic Recovery**: Healthy devices are automatically reported as available
- **Detailed Logging**: Health check results are logged with counts

**Health Check Logic**:

```go
func GetDeviceHealth(deviceID string) bool {
    device := getDevice(deviceID)
    return checkDeviceExists(device.Path) && checkDeviceReadable(device.Path)
}

// In ListAndWatch - reports health status for each device
for _, device := range allDevices {
    deviceHealthy := p.v4l2Manager.GetDeviceHealth(device.ID)
    health := pluginapi.Healthy
    if !deviceHealthy {
        health = pluginapi.Unhealthy
    }
    // Send to Kubernetes...
}
```

## 🚀 Features

### Core Features

- **8 Virtual Devices per Node**: Configurable device count (max 8)
- **Automatic Device Creation**: v4l2loopback module loading and device setup
- **Kubernetes-Native Allocation**: Follows official device plugin patterns (like GPU plugins)
- **Per-Device Health Monitoring**: Individual device health status reporting
- **Real-time Health Updates**: Immediate notification when devices become unhealthy
- **Thread-Safe Operations**: Mutex-protected device state management
- **No Complex Tracking**: Leverages Kubernetes' built-in device management
- **Dynamic Device Reset**: Automatic device refresh between allocations using v4l2loopback-ctl
- **Fresh Device State**: Every pod gets a clean, reset device to prevent unresponsive states

### Dynamic Device Management Feature

- **Automatic Device Reset**: Every pod allocation triggers a device reset (delete + recreate)
- **v4l2loopback-ctl Integration**: Uses the latest v4l2loopback-ctl (v0.15.1) for device management
- **Fresh Device State**: Eliminates unresponsive device issues between allocations
- **PreStartContainer Hook**: Leverages Kubernetes PreStartContainer for device reset timing
- **Configuration Preservation**: Recreates devices with same parameters (buffers, caps, labels)
- **Fallback Mode Support**: Skips device reset when in fallback mode (dummy devices)
- **Error Handling**: Comprehensive error handling and logging for device reset operations

### Fallback Mode Feature

- **Graceful Degradation**: Automatically switches to dummy device mode when kernel modules fail
- **Kernel Header Mismatch Handling**: Prevents pod scheduling failures due to missing kernel headers
- **Dummy Device Paths**: Creates actual device files `/dev/dummy-video10`, `/dev/dummy-video11`, etc. that can be mounted by Kubernetes
- **Application-Friendly**: Applications receive device paths they can handle gracefully
- **Comprehensive Logging**: Clear indication when running in fallback mode with reason
- **Configurable Fallback**: Can be disabled or customized via environment variables

### Advanced Features

- **Structured Logging**: JSON-formatted logs with configurable levels
- **Configuration Management**: Environment variable based configuration
- **Error Handling**: Comprehensive error handling and recovery
- **Graceful Shutdown**: Proper cleanup on pod termination
- **Kubelet Restart Recovery**: Automatically detects and re-registers after kubelet restarts
- **Health Check Logging**: Detailed logging of healthy/unhealthy device counts
- **Device Isolation**: Automatic prevention of device conflicts between pods

## 📋 Prerequisites

### System Requirements

- **Operating System**: Ubuntu 24.04 LTS
- **Kernel Version**: 6.8.0-85-generic
- **Kubernetes**: 1.33.4
- **Container Runtime**: Docker/container with privileged mode support

> **Note**: While these are the hard prerequisites for this repository, the device plugin can be configured for other systems. For different kernel and Ubuntu versions, the Dockerfile can be updated to download Linux headers for the required kernel version. Go dependencies should be changed based on the desired Kubernetes version. However, this module has been tested and verified on the above configuration.

### Required Kernel Modules

```bash
# Check if required modules are available
lsmod | grep videodev
lsmod | grep v4l2loopback

# Install if missing
sudo apt-get update
sudo apt-get install linux-modules-extra-6.8.0-85-generic
sudo apt-get install v4l2loopback-dkms v4l2loopback-utils
```

**Note**: This plugin automatically installs and uses v4l2loopback v0.15.1 (latest version) from source during the Docker build process. The older Ubuntu package version (0.6) is replaced to ensure compatibility with dynamic device management features.

### Kubernetes Requirements

- **DaemonSet Support**: For running on every node
- **Privileged Containers**: Required for kernel module access
- **Host Network**: Required for device plugin communication
- **RBAC**: Proper permissions for pod and node access

## 🛠️ Configuration

### Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
# Required
NODE_NAME=worker-node-1

# Optional (with defaults)
MAX_DEVICES=8
LOG_LEVEL=info
RESOURCE_NAME=meeting-baas.io/video-devices
KUBELET_SOCKET=/var/lib/kubelet/device-plugins/kubelet.sock
SOCKET_PATH=/var/lib/kubelet/device-plugins/video-device-plugin.sock

# V4L2 Configuration
V4L2_MAX_BUFFERS=2
V4L2_EXCLUSIVE_CAPS=1
V4L2_CARD_LABEL=MeetingBot_WebCam
V4L2_DEVICE_PERM=0666

# Fallback Configuration
ENABLE_FALLBACK_MODE=true
FALLBACK_DEVICE_PREFIX=/dev/dummy-video

# Monitoring
ENABLE_METRICS=false
METRICS_PORT=8080
HEALTH_CHECK_INTERVAL=30
```

### Configuration Details

| Variable | Description | Default | Range |
|----------|-------------|---------|-------|
| `NODE_NAME` | Kubernetes node name | Required | String |
| `MAX_DEVICES` | Devices per node | 8 | 1-8 |
| `LOG_LEVEL` | Logging level | info | debug/info/warn/error |
| `RESOURCE_NAME` | K8s resource name | meeting-baas.io/video-devices | String |
| `V4L2_CARD_LABEL` | Device label | MeetingBot_WebCam | String |
| `V4L2_DEVICE_PERM` | Device permissions (octal) | 0666 | 0600-0777 |
| `ENABLE_FALLBACK_MODE` | Enable fallback mode on kernel module failure | true | true/false |
| `FALLBACK_DEVICE_PREFIX` | Prefix for dummy device paths in fallback mode | /dev/dummy-video | String |

### Security Considerations

The `V4L2_DEVICE_PERM` setting controls file permissions for video devices:

| Permission | Description | Use Case |
|------------|-------------|----------|
| `0666` (default) | `rw-rw-rw-` | Development, testing, shared access |
| `0644` | `rw-r--r--` | Production with read-only access for non-owners |
| `0600` | `rw-------` | High security, owner-only access |
| `0640` | `rw-r-----` | Group access for specific users |

**Recommendations:**

- **Development**: Use `0666` for maximum compatibility
- **Production**: Use `0644` or `0600` for better security
- **Multi-tenant**: Use `0640` with proper group management

## 🐳 Building and Deployment

### 1. Build the Docker Image

```bash
# Build the image
docker build -t video-device-plugin:latest .

# Or use the build script
./docker-build.sh
```

### 2. Kubernetes Deployment

#### DaemonSet Configuration

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: video-device-plugin
  namespace: kube-system
spec:
  selector:
    matchLabels:
      name: video-device-plugin
  template:
    metadata:
      labels:
        name: video-device-plugin
    spec:
      serviceAccountName: video-device-plugin
      hostNetwork: true
      hostPID: true
      containers:
      - name: video-device-plugin
        image: your-registry/video-device-plugin:latest
        securityContext:
          privileged: true
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: MAX_DEVICES
          value: "8"
        - name: LOG_LEVEL
          value: "info"
        volumeMounts:
        - name: device-plugins
          mountPath: /var/lib/kubelet/device-plugins
        - name: dev
          mountPath: /dev
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "128Mi"
            cpu: "200m"
        livenessProbe:
          exec:
            command:
            - /bin/sh
            - -c
            - 'count=$(ls /dev/video* 2>/dev/null | wc -l); [ "$count" -ge "${MAX_DEVICES:-8}" ]'
          initialDelaySeconds: 30
          periodSeconds: 30
        readinessProbe:
          exec:
            command:
            - /bin/sh
            - -c
            - 'count=$(ls /dev/video* 2>/dev/null | wc -l); [ "$count" -ge "${MAX_DEVICES:-8}" ]'
          initialDelaySeconds: 10
          periodSeconds: 10
      volumes:
      - name: device-plugins
        hostPath:
          path: /var/lib/kubelet/device-plugins
      - name: dev
        hostPath:
          path: /dev
```

#### RBAC Configuration

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: video-device-plugin
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: video-device-plugin
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: video-device-plugin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: video-device-plugin
subjects:
- kind: ServiceAccount
  name: video-device-plugin
  namespace: kube-system
```

**Note**: No pod permissions needed - the simplified architecture lets Kubernetes handle device lifecycle management entirely.

### 3. Using the Plugin

#### ScaledJob Configuration

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledJob
metadata:
  name: video-processing-job
spec:
  jobTargetRef:
    template:
      spec:
        containers:
        - name: video-processor
          image: your-app:latest
          resources:
            requests:
              meeting-baas.io/video-devices: 1
            limits:
              meeting-baas.io/video-devices: 1
```

#### Regular Pod Configuration

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: video-app
spec:
  containers:
  - name: video-app
    image: your-app:latest
    resources:
      requests:
        meeting-baas.io/video-devices: 1
      limits:
        meeting-baas.io/video-devices: 1
```

## 🔍 Monitoring and Troubleshooting

### Health Checks

```bash
# Check DaemonSet status
kubectl get daemonset -n kube-system video-device-plugin

# Check pod logs (look for health check messages)
kubectl logs -n kube-system -l name=video-device-plugin | grep "Health check completed"

# Check device creation
kubectl debug node/<node-name> -it --image=busybox -- chroot /host ls -la /dev/video*

# Check device plugin registration and health status
kubectl get nodes -o jsonpath='{.items[*].status.allocatable}' | jq

# Check specific device health in logs
kubectl logs -n kube-system -l name=video-device-plugin | grep "device health check failed"

# Check device permissions
kubectl debug node/<node-name> -it --image=busybox -- chroot /host ls -la /dev/video*
```

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| Pods stuck in Pending | DaemonSet not running | Check DaemonSet status and logs |
| No video devices | v4l2loopback not loaded | Check kernel module loading in logs |
| Permission denied | Missing privileged mode | Ensure `privileged: true` in DaemonSet |
| Permission denied | Device permissions too restrictive | Check `V4L2_DEVICE_PERM` setting and adjust if needed |
| Device allocation fails | All devices busy | Check device utilization and scaling |
| Plugin stops working after kubelet restart | Kubelet restart not detected | Plugin auto-re-registers, check logs for re-registration |
| Devices reported as unhealthy | Device files missing/corrupted | Check device creation and permissions in logs |
| Health check failures | Device access issues | Verify device permissions and v4l2loopback status |
| Plugin enters fallback mode | Kernel header mismatch | Check logs for fallback reason, ensure correct kernel headers are installed |
| Applications receive dummy device paths | Fallback mode active | This is expected behavior - applications should handle gracefully |
| Device reset fails | v4l2loopback-ctl not found | Check that v4l2loopback-ctl is installed and control device exists |
| Device reset fails | Control device missing | Check that `/dev/v4l2loopback` exists and module is loaded correctly |
| PreStartContainer errors | Device reset timeout | Check device reset logs and v4l2loopback-ctl output |

### Fallback Mode Troubleshooting

When the plugin enters fallback mode, you'll see logs like:

```json
{
  "time": "2024-01-15T10:30:00Z",
  "level": "WARN",
  "msg": "Video device plugin running in fallback mode",
  "reason": "videodev module not found - kernel headers mismatch",
  "dummy_devices": 4,
  "fallback_prefix": "/dev/dummy-video"
}
```

**Common Fallback Scenarios:**
- **Kernel header mismatch**: `linux-modules-extra-$(uname -r)` not installed
- **Module loading timeout**: Kernel module loading takes too long
- **Permission issues**: Insufficient privileges to load kernel modules

**Handling Fallback Mode:**
1. **For Applications**: Check for dummy device paths and handle gracefully
2. **For Operators**: Monitor logs for fallback mode activation with structured error details
3. **For Debugging**: Check structured error logs with original error information
4. **Device Files**: Fallback devices are created as symbolic links to `/dev/null` for Kubernetes mounting

### Logging

The plugin uses structured JSON logging with health monitoring:

```json
{
  "time": "2024-01-15T10:30:00Z",
  "level": "INFO",
  "msg": "Found video devices",
  "device_count": 8,
  "healthy_count": 7,
  "unhealthy_count": 1
}

{
  "time": "2024-01-15T10:30:15Z",
  "level": "INFO",
  "msg": "Device allocated",
  "device_id": "video10",
  "device_path": "/dev/video10",
  "container_path": "/dev/video10",
  "env_var": "VIDEO_DEVICE=/dev/video10"
}

{
  "time": "2024-01-15T10:30:30Z",
  "level": "WARN",
  "msg": "Device health check failed",
  "device_id": "video15",
  "device_path": "/dev/video15"
}

{
  "time": "2024-01-15T10:31:00Z",
  "level": "INFO",
  "msg": "PreStartContainer called",
  "devices": ["video10"]
}

{
  "time": "2024-01-15T10:31:01Z",
  "level": "INFO",
  "msg": "Resetting device",
  "device_id": "video10",
  "device_path": "/dev/video10"
}

{
  "time": "2024-01-15T10:31:02Z",
  "level": "INFO",
  "msg": "Device reset successfully",
  "device_id": "video10",
  "device_path": "/dev/video10"
}
```

## 🤝 Contributing

This project is open source and welcomes contributions! Areas where help is needed:

- **Additional kernel support**: Support for other Linux distributions
- **Device types**: Support for other V4L2 device types
- **Monitoring**: Prometheus metrics and Grafana dashboards
- **Testing**: End-to-end testing with real Kubernetes clusters
- **Documentation**: Additional examples and use cases

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- **v4l2loopback**: The kernel module that makes virtual video devices possible
- **Kubernetes Device Plugin API**: The framework that enables device management
- **Community**: All the developers who struggled with video devices in containers

## 📞 Support

If you're facing issues or have questions:

1. **Check the logs**: Look for error messages in DaemonSet pod logs
2. **Verify prerequisites**: Ensure kernel modules and permissions are correct
3. **Open an issue**: Create a GitHub issue with detailed information
4. **Community**: Join discussions in the project's GitHub Discussions
