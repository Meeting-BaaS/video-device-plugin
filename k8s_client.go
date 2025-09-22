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
	k.logger.Info("Pod completed, releasing devices", 
		"pod_name", pod.Name,
		"pod_namespace", pod.Namespace,
		"pod_phase", pod.Status.Phase)

	// For now, we'll implement a simple approach
	// In a full implementation, we'd need to track which device was allocated to which pod
	k.releaseAllDevices()
}

// handlePodDeletion handles when a pod is deleted and releases its devices
func (k *K8sClient) handlePodDeletion(pod *v1.Pod) {
	k.logger.Info("Pod deleted, releasing devices", 
		"pod_name", pod.Name,
		"pod_namespace", pod.Namespace)

	// For now, we'll implement a simple approach
	// In a full implementation, we'd need to track which device was allocated to which pod
	k.releaseAllDevices()
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

// releaseAllDevices releases all allocated devices (temporary implementation)
func (k *K8sClient) releaseAllDevices() {
	k.logger.Info("Releasing all allocated devices")
	
	// Get all devices and release them
	devices := k.devicePlugin.v4l2Manager.ListAllDevices()
	for deviceID, device := range devices {
		if device.Allocated {
			if err := k.devicePlugin.v4l2Manager.ReleaseDevice(deviceID); err != nil {
				k.logger.Error("Failed to release device", "device_id", deviceID, "error", err)
			} else {
				k.logger.Info("Released device", "device_id", deviceID)
			}
		}
	}
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
