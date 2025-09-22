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
2. Kubelet queries device plugin for available devices
3. Device plugin allocates first available device (e.g., video10)
4. Device plugin returns device info + annotation to kubelet
5. Kubelet applies annotation to pod
6. Pod starts with VIDEO_DEVICE=/dev/video10
7. Pod completes → Device plugin releases device via annotation
```

## 🔧 Key Concepts

### 1. Device Plugin Architecture

The plugin implements the **Kubernetes Device Plugin API** with these key methods:

- **`ListAndWatch`**: Streams available devices to kubelet
- **`Allocate`**: Allocates specific devices to pods
- **`GetDevicePluginOptions`**: Returns plugin configuration
- **`PreStartContainer`**: Optional pre-start container logic

### 2. Annotation-Based Device Tracking

Instead of complex pod-to-device mapping, we use **Kubernetes annotations**:

```yaml
# Pod gets annotated automatically by kubelet
metadata:
  annotations:
    meeting-baas.io/video-device-id: "video10"
```

**Benefits**:

- No pod identification needed during allocation
- Kubelet handles annotation application
- No additional RBAC permissions required
- Simple and reliable device tracking

### 3. V4L2 Device Management

**Device Range**: `/dev/video10` to `/dev/video17` (8 devices per node)

- Starts from `video10` to avoid conflicts with system devices
- Each device can be allocated to exactly one pod
- Thread-safe allocation with mutex protection
- Automatic device health monitoring

**Device Lifecycle**:

```text
Create → Allocate → Use → Release → Available
   ↓         ↓        ↓       ↓         ↓
Startup   Pod Req   Pod Run  Pod End   Ready
```

### 4. Pod Completion Detection

**Watch API Strategy**:

- **Pod Completion**: Watch for phase changes to `Succeeded`/`Failed`
- **Pod Deletion**: Watch for pod deletion events
- **Startup Recovery**: Query all running pods with annotations on restart

**Release Logic**:

```go
if pod.Annotations["meeting-baas.io/video-device-id"] {
    if pod.phase == "Succeeded" || pod.phase == "Failed" {
        releaseDevice(deviceID)
        addToCompletedList(pod.UID)
    } else if pod.deleted && !inCompletedList(pod.UID) {
        releaseDevice(deviceID) // Pod crashed/deleted
    }
}
```

## 🚀 Features

### Core Features

- **8 Virtual Devices per Node**: Configurable device count (max 8)
- **Automatic Device Creation**: v4l2loopback module loading and device setup
- **Resource Allocation**: One device per pod with conflict prevention
- **Automatic Scaling**: Works with Kubernetes autoscaling
- **Health Monitoring**: Continuous device availability checking
- **Graceful Shutdown**: Proper cleanup on pod termination

### Advanced Features

- **Annotation-Based Tracking**: Simple and reliable device management
- **Startup Recovery**: Rebuilds device state from Kubernetes API
- **Structured Logging**: JSON-formatted logs with configurable levels
- **Configuration Management**: Environment variable based configuration
- **Error Handling**: Comprehensive error handling and recovery
- **Thread Safety**: Mutex-protected device operations

## 📋 Prerequisites

### System Requirements

- **Operating System**: Ubuntu 24.04 LTS
- **Kernel Version**: 6.8.0-64-generic (or compatible)
- **Kubernetes**: 1.33.4
- **Container Runtime**: Docker/container with privileged mode support

### Required Kernel Modules

```bash
# Check if required modules are available
lsmod | grep videodev
lsmod | grep v4l2loopback

# Install if missing
sudo apt-get update
sudo apt-get install linux-modules-extra-6.8.0-64-generic
sudo apt-get install v4l2loopback-dkms v4l2loopback-utils
```

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
            - "ls /dev/video* | wc -l | grep -q 8"
          initialDelaySeconds: 30
          periodSeconds: 30
        readinessProbe:
          exec:
            command:
            - /bin/sh
            - -c
            - "ls /dev/video* | wc -l | grep -q 8"
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
- apiGroups: [""]
  resources: ["pods"]
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

# Check pod logs
kubectl logs -n kube-system -l name=video-device-plugin

# Check device creation
kubectl debug node/<node-name> -it --image=busybox -- chroot /host ls -la /dev/video*

# Check device plugin registration
kubectl get nodes -o jsonpath='{.items[*].status.allocatable}' | jq
```

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| Pods stuck in Pending | DaemonSet not running | Check DaemonSet status and logs |
| No video devices | v4l2loopback not loaded | Check kernel module loading in logs |
| Permission denied | Missing privileged mode | Ensure `privileged: true` in DaemonSet |
| Device allocation fails | All devices busy | Check device utilization and scaling |

### Logging

The plugin uses structured JSON logging:

```json
{
  "time": "2024-01-15T10:30:00Z",
  "level": "INFO",
  "msg": "Device allocated",
  "device_id": "video10",
  "device_path": "/dev/video10",
  "pod": "video-app-123",
  "node_name": "worker-node-1"
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
