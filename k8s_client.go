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
	clientset        *kubernetes.Clientset
	logger           *slog.Logger
	stopCh           chan struct{}
	mu               sync.RWMutex
	devicePlugin     *VideoDevicePlugin
	// Track completed pods to avoid double device release
	podsCompletedList map[string]bool // podUID -> true
}

// NewK8sClient creates a new Kubernetes client
func NewK8sClient(logger *slog.Logger, devicePlugin *VideoDevicePlugin) (*K8sClient, error) {
	clientset, err := createK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &K8sClient{
		clientset:         clientset,
		logger:            logger,
		stopCh:            make(chan struct{}),
		devicePlugin:      devicePlugin,
		podsCompletedList: make(map[string]bool),
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

	// Query all running pods
	pods, err := k.clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		k.logger.Error("Failed to list pods for startup reconciliation", "error", err)
		return
	}

	// Find pods with our video device annotation and mark devices as allocated
	allocatedDevices := 0
	for _, pod := range pods.Items {
		if deviceID, exists := pod.Annotations["meeting-baas.io/video-device-id"]; exists {
			// Mark this device as allocated in our V4L2 manager
			if err := k.devicePlugin.v4l2Manager.MarkDeviceAsAllocated(deviceID); err != nil {
				k.logger.Error("Failed to mark device as allocated during startup reconciliation", 
					"device_id", deviceID, "pod", pod.Name, "namespace", pod.Namespace, "error", err)
			} else {
				k.logger.Info("Marked device as allocated during startup reconciliation", 
					"device_id", deviceID, "pod", pod.Name, "namespace", pod.Namespace)
				allocatedDevices++
			}
		}
	}

	k.logger.Info("Startup reconciliation completed", 
		"total_pods", len(pods.Items), 
		"allocated_devices", allocatedDevices)
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
				
				// Check if pod transitioned to Completed or Failed
				if (oldPod.Status.Phase != v1.PodSucceeded && newPod.Status.Phase == v1.PodSucceeded) ||
				   (oldPod.Status.Phase != v1.PodFailed && newPod.Status.Phase == v1.PodFailed) {
					k.handlePodCompletion(newPod)
				}
			},
			DeleteFunc: func(obj interface{}) {
				pod := obj.(*v1.Pod)
				k.handlePodDeletion(pod)
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
	// Check if pod has our video device annotation
	deviceID, exists := pod.Annotations["meeting-baas.io/video-device-id"]
	if !exists {
		return // Not our pod, ignore
	}

	k.logger.Info("Pod completed, releasing device", 
		"pod_name", pod.Name,
		"pod_namespace", pod.Namespace,
		"pod_phase", pod.Status.Phase,
		"device_id", deviceID)

	// Release the specific device
	if err := k.devicePlugin.v4l2Manager.ReleaseDevice(deviceID); err != nil {
		k.logger.Error("Failed to release device for completed pod", 
			"device_id", deviceID, "pod", pod.Name, "error", err)
	} else {
		k.logger.Info("Released device for completed pod", 
			"device_id", deviceID, "pod", pod.Name)
	}

	// Add to completed list to avoid double release on deletion
	k.mu.Lock()
	k.podsCompletedList[string(pod.UID)] = true
	k.mu.Unlock()
}

// handlePodDeletion handles when a pod is deleted and releases its devices
func (k *K8sClient) handlePodDeletion(pod *v1.Pod) {
	// Check if pod has our video device annotation
	deviceID, exists := pod.Annotations["meeting-baas.io/video-device-id"]
	if !exists {
		return // Not our pod, ignore
	}

	k.mu.Lock()
	wasCompleted := k.podsCompletedList[string(pod.UID)]
	if wasCompleted {
		// Device already released during completion, just clean up
		delete(k.podsCompletedList, string(pod.UID))
		k.mu.Unlock()
		k.logger.Info("Pod deleted (was completed), cleaned up tracking", 
			"pod_name", pod.Name, "device_id", deviceID)
		return
	}
	k.mu.Unlock()

	// Pod was deleted without completing (crashed/force deleted), release device
	k.logger.Info("Pod deleted (not completed), releasing device", 
		"pod_name", pod.Name,
		"pod_namespace", pod.Namespace,
		"device_id", deviceID)

	if err := k.devicePlugin.v4l2Manager.ReleaseDevice(deviceID); err != nil {
		k.logger.Error("Failed to release device for deleted pod", 
			"device_id", deviceID, "pod", pod.Name, "error", err)
	} else {
		k.logger.Info("Released device for deleted pod", 
			"device_id", deviceID, "pod", pod.Name)
	}
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

