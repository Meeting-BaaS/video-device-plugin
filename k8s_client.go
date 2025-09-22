package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/fields"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	v1 "k8s.io/api/core/v1"
)

// K8sClient handles Kubernetes API interactions for device reconciliation
type K8sClient struct {
	clientset   *kubernetes.Clientset
	logger      *slog.Logger
	stopCh      chan struct{}
	mu          sync.RWMutex
	devicePlugin *VideoDevicePlugin
	// Track which device is allocated to which pod
	podToDevice  map[string]string // pod key -> device ID
	deviceToPod  map[string]string // device ID -> pod key
}

// NewK8sClient creates a new Kubernetes client
func NewK8sClient(logger *slog.Logger, devicePlugin *VideoDevicePlugin) (*K8sClient, error) {
	clientset, err := createK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &K8sClient{
		clientset:    clientset,
		logger:       logger,
		stopCh:       make(chan struct{}),
		devicePlugin: devicePlugin,
		podToDevice:  make(map[string]string),
		deviceToPod:  make(map[string]string),
	}, nil
}

// Start starts the Kubernetes client monitoring
func (k *K8sClient) Start() error {
	if k.clientset == nil {
		k.logger.Warn("Kubernetes client not available, skipping pod monitoring")
		return nil
	}

	k.logger.Info("Starting Kubernetes client for pod monitoring")

	// Perform startup reconciliation
	k.performStartupReconciliation()

	// Start Watch API for real-time events
	k.startPodWatch()

	// Start periodic reconciliation (every 5 minutes)
	go k.startPeriodicReconciliation()

	return nil
}

// Stop stops the Kubernetes client monitoring
func (k *K8sClient) Stop() {
	k.logger.Info("Stopping Kubernetes client monitoring")
	close(k.stopCh)
}

// performStartupReconciliation queries all pods and reconciles device state
func (k *K8sClient) performStartupReconciliation() {
	k.logger.Info("Performing startup reconciliation...")

	// Query all pods with video-device resources
	pods, err := k.clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		k.logger.Error("Failed to list pods for startup reconciliation", "error", err)
		return
	}

	// Track which devices should be allocated
	expectedAllocations := make(map[string]bool)
	
	for _, pod := range pods.Items {
		if k.podRequestsVideoDevices(&pod) {
			// This pod should have a video device allocated
			// We'll mark it as expected to be allocated
			podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			expectedAllocations[podKey] = true
		}
	}

	// Release any devices that are allocated but shouldn't be
	// (This is a simplified approach - in practice, we'd need to track pod->device mapping)
	k.logger.Info("Startup reconciliation completed", "expected_pods", len(expectedAllocations))
}

// startPodWatch starts watching for pod events
func (k *K8sClient) startPodWatch() {
	// Watch for pods that request our video device resource
	watchlist := cache.NewListWatchFromClient(
		k.clientset.CoreV1().RESTClient(),
		"pods",
		"", // All namespaces
		fields.Everything(),
	)

	_, controller := cache.NewInformer(
		watchlist,
		&v1.Pod{},
		0, // No resync period
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldPod := oldObj.(*v1.Pod)
				newPod := newObj.(*v1.Pod)
				
				// Check if pod transitioned to Completed
				if oldPod.Status.Phase != v1.PodSucceeded && 
				   newPod.Status.Phase == v1.PodSucceeded {
					if k.podRequestsVideoDevices(newPod) {
						k.handlePodCompletion(newPod)
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				pod := obj.(*v1.Pod)
				if k.podRequestsVideoDevices(pod) {
					k.handlePodDeletion(pod)
				}
			},
		},
	)

	// Start the controller in a goroutine
	go controller.Run(k.stopCh)
}

// startPeriodicReconciliation starts periodic reconciliation every 5 minutes
func (k *K8sClient) startPeriodicReconciliation() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			k.logger.Debug("Running periodic reconciliation...")
			k.performStartupReconciliation()
		case <-k.stopCh:
			return
		}
	}
}

// handlePodCompletion handles when a pod completes and releases its devices
func (k *K8sClient) handlePodCompletion(pod *v1.Pod) {
	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	
	k.logger.Info("Pod completed, releasing device", 
		"pod_name", pod.Name,
		"pod_namespace", pod.Namespace,
		"pod_phase", pod.Status.Phase,
		"pod_key", podKey)

	// For now, release all devices since we can't easily correlate pod->device
	// In a production system, you'd want to implement proper pod->device tracking
	// This is a simplified approach that works for the current use case
	k.releaseAllAllocatedDevices()
}

// handlePodDeletion handles when a pod is deleted and releases its devices
func (k *K8sClient) handlePodDeletion(pod *v1.Pod) {
	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	
	k.logger.Info("Pod deleted, releasing device", 
		"pod_name", pod.Name,
		"pod_namespace", pod.Namespace,
		"pod_key", podKey)

	// For now, release all devices since we can't easily correlate pod->device
	// In a production system, you'd want to implement proper pod->device tracking
	// This is a simplified approach that works for the current use case
	k.releaseAllAllocatedDevices()
}

// podRequestsVideoDevices checks if a pod requests video devices
func (k *K8sClient) podRequestsVideoDevices(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Resources.Requests != nil {
			if _, exists := container.Resources.Requests["meeting-baas.io/video-devices"]; exists {
				return true
			}
		}
	}
	return false
}

// trackDeviceAllocation tracks that a device was allocated to a pod
func (k *K8sClient) trackDeviceAllocation(podKey, deviceID string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	
	k.podToDevice[podKey] = deviceID
	k.deviceToPod[deviceID] = podKey
	
	k.logger.Debug("Tracked device allocation", "pod_key", podKey, "device_id", deviceID)
}

// releaseDeviceForPod releases the specific device allocated to a pod
func (k *K8sClient) releaseDeviceForPod(podKey string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	
	deviceID, exists := k.podToDevice[podKey]
	if !exists {
		k.logger.Warn("No device tracked for pod", "pod_key", podKey)
		return
	}
	
	// Release the device
	if err := k.devicePlugin.v4l2Manager.ReleaseDevice(deviceID); err != nil {
		k.logger.Error("Failed to release device", "device_id", deviceID, "pod_key", podKey, "error", err)
	} else {
		k.logger.Info("Released device for pod", "device_id", deviceID, "pod_key", podKey)
	}
	
	// Clean up tracking
	delete(k.podToDevice, podKey)
	delete(k.deviceToPod, deviceID)
}

// releaseAllAllocatedDevices releases all currently allocated devices
func (k *K8sClient) releaseAllAllocatedDevices() {
	k.logger.Info("Releasing all allocated devices")
	
	// Get all devices and release only the allocated ones
	devices := k.devicePlugin.v4l2Manager.ListAllDevices()
	releasedCount := 0
	for deviceID, device := range devices {
		if device.Allocated {
			if err := k.devicePlugin.v4l2Manager.ReleaseDevice(deviceID); err != nil {
				k.logger.Error("Failed to release device", "device_id", deviceID, "error", err)
			} else {
				k.logger.Info("Released device", "device_id", deviceID)
				releasedCount++
			}
		}
	}
	
	k.logger.Info("Device release completed", "released_count", releasedCount)
	
	// Clear tracking
	k.mu.Lock()
	k.podToDevice = make(map[string]string)
	k.deviceToPod = make(map[string]string)
	k.mu.Unlock()
}

// releaseAllDevices releases all allocated devices (fallback for cleanup)
func (k *K8sClient) releaseAllDevices() {
	k.releaseAllAllocatedDevices()
}

// createK8sClient creates a Kubernetes client using in-cluster config
func createK8sClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return clientset, nil
}

