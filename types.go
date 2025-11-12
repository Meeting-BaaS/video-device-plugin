package main

import (
	"fmt"
	"time"
)

// VideoDevice represents a virtual video device
type VideoDevice struct {
	ID   string `json:"id"`   // Device ID (e.g., "video0")
	Path string `json:"path"` // Device path (e.g., "/dev/video0")
}

// DevicePluginConfig holds configuration for the device plugin
type DevicePluginConfig struct {
	// Core Configuration
	MaxDevices    int    `json:"max_devices"`    // Maximum number of video devices
	NodeName      string `json:"node_name"`      // Kubernetes node name
	KubeletSocket string `json:"kubelet_socket"` // Path to kubelet socket
	ResourceName  string `json:"resource_name"`  // Resource name for device plugin
	SocketPath    string `json:"socket_path"`    // Path to device plugin socket
	LogLevel      string `json:"log_level"`      // Log level (debug, info, warn, error)

	// Development/Debugging
	Debug bool `json:"debug"` // Enable debug mode

	// V4L2 Configuration
	V4L2MaxBuffers    int    `json:"v4l2_max_buffers"`    // Number of buffers for v4l2loopback
	V4L2ExclusiveCaps int    `json:"v4l2_exclusive_caps"` // Enable exclusive capabilities (0,1) 0 is default and false, 1 is true
	V4L2CardLabel     string `json:"v4l2_card_label"`     // Card label for devices
	V4L2DevicePerm    int    `json:"v4l2_device_perm"`    // Device permissions (octal, e.g., 0666)

	// Kubernetes Integration
	KubernetesNamespace string `json:"kubernetes_namespace"` // Namespace for deployment
	ServiceAccountName  string `json:"service_account_name"` // Service account name

	// Monitoring and Observability
	EnableMetrics       bool `json:"enable_metrics"`        // Enable Prometheus metrics
	MetricsPort         int  `json:"metrics_port"`          // Metrics port
	HealthCheckInterval int  `json:"health_check_interval"` // Health check interval in seconds

	// Performance Tuning
	AllocationTimeout     int `json:"allocation_timeout"`      // Device allocation timeout in seconds
	DeviceCreationTimeout int `json:"device_creation_timeout"` // Device creation timeout in seconds
	ShutdownTimeout       int `json:"shutdown_timeout"`        // Graceful shutdown timeout in seconds
	CleanupTimeout        int `json:"cleanup_timeout"`         // Module cleanup timeout in seconds

	// Fallback Configuration
	EnableFallbackMode   bool   `json:"enable_fallback_mode"`   // Enable fallback mode when kernel modules fail
	FallbackDevicePrefix string `json:"fallback_device_prefix"` // Prefix for dummy device paths
	FallbackModeReason   string `json:"fallback_mode_reason"`   // Reason for entering fallback mode
}

// V4L2Manager interface for managing V4L2 devices
type V4L2Manager interface {
	// CreateDevices creates the specified number of video devices
	CreateDevices(count int) error

	// GetDeviceByID returns a device by its ID
	GetDeviceByID(deviceID string) (*VideoDevice, error)

	// IsHealthy checks if the V4L2 system is healthy
	IsHealthy(maxDevices int) bool

	// GetDeviceCount returns the total number of devices
	GetDeviceCount(maxDevices int) int

	// ListAllDevices returns all devices
	ListAllDevices() map[string]*VideoDevice

	// GetDeviceHealth returns health status for a specific device
	GetDeviceHealth(deviceID string) bool

	// IsFallbackMode returns true if the manager is in fallback mode
	IsFallbackMode() bool

	// GetFallbackReason returns the reason for fallback mode
	GetFallbackReason() string

	// EnableFallbackMode enables fallback mode and creates dummy devices
	EnableFallbackMode(reason string, count int) error

	// CleanupFallbackDevices removes the fallback device files
	CleanupFallbackDevices()
}

// DevicePluginServer interface for the gRPC device plugin server
type DevicePluginServer interface {
	// Start starts the device plugin server
	Start() error

	// Stop stops the device plugin server
	Stop() error

	// WaitForShutdown waits for shutdown signal
	WaitForShutdown()

	// RegisterWithKubelet registers the device plugin with kubelet
	RegisterWithKubelet() error
}

// HealthCheck represents the health status of the device plugin
type HealthCheck struct {
	Healthy      bool      `json:"healthy"`
	V4L2Healthy  bool      `json:"v4l2_healthy"`
	DevicesReady bool      `json:"devices_ready"`
	LastChecked  time.Time `json:"last_checked"`
	Errors       []string  `json:"errors,omitempty"`
}

// ModuleLoadError represents an error that occurred during kernel module loading
type ModuleLoadError struct {
	Module               string `json:"module"`
	Reason               string `json:"reason"`
	Original             error  `json:"-"`                                // Omit from JSON to avoid serialization issues and potential leaks
	OriginalErrorMessage string `json:"original_error_message,omitempty"` // String representation for JSON logging
	CanFallback          bool   `json:"can_fallback"`
}

func (e *ModuleLoadError) Error() string {
	return fmt.Sprintf("failed to load %s module: %s (original: %v)", e.Module, e.Reason, e.Original)
}

func (e *ModuleLoadError) Unwrap() error {
	return e.Original
}
