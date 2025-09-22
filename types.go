package main

import (
	"time"
)

// VideoDevice represents a virtual video device
type VideoDevice struct {
	ID          string    `json:"id"`          // Device ID (e.g., "video0")
	Path        string    `json:"path"`        // Device path (e.g., "/dev/video0")
	Allocated   bool      `json:"allocated"`   // Whether device is currently allocated
	AllocatedAt time.Time `json:"allocated_at"` // When device was allocated
}

// DevicePluginConfig holds configuration for the device plugin
type DevicePluginConfig struct {
	MaxDevices      int    `json:"max_devices"`       // Maximum number of video devices
	NodeName        string `json:"node_name"`         // Kubernetes node name
	KubeletSocket   string `json:"kubelet_socket"`    // Path to kubelet socket
	ResourceName    string `json:"resource_name"`     // Resource name for device plugin
	SocketPath      string `json:"socket_path"`       // Path to device plugin socket
	LogLevel        string `json:"log_level"`         // Log level (debug, info, warn, error)
}

// V4L2Manager interface for managing V4L2 devices
type V4L2Manager interface {
	// LoadModule loads the v4l2loopback kernel module
	LoadModule() error
	
	// CreateDevices creates the specified number of video devices
	CreateDevices(count int) error
	
	// GetAvailableDevices returns a list of unallocated devices
	GetAvailableDevices() []*VideoDevice
	
	// AllocateDevice allocates a device and returns it
	AllocateDevice() (*VideoDevice, error)
	
	// ReleaseDevice releases a device back to the pool
	ReleaseDevice(deviceID string) error
	
	// IsHealthy checks if the V4L2 system is healthy
	IsHealthy() bool
	
	// GetDeviceCount returns the total number of devices
	GetDeviceCount() int
	
	// GetAllocatedDeviceCount returns the number of allocated devices
	GetAllocatedDeviceCount() int
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

// DeviceAllocationRequest represents a request to allocate a device
type DeviceAllocationRequest struct {
	DeviceType string `json:"device_type"` // Always "video" for our use case
}

// DeviceAllocationResponse represents the response to a device allocation request
type DeviceAllocationResponse struct {
	Device     *VideoDevice `json:"device"`
	Success    bool         `json:"success"`
	Error      string       `json:"error,omitempty"`
	EnvVars    []string     `json:"env_vars"` // Environment variables to set
	Mounts     []string     `json:"mounts"`   // Device mounts for the pod
}

// DeviceStatus represents the current status of all devices
type DeviceStatus struct {
	TotalDevices      int            `json:"total_devices"`
	AvailableDevices  int            `json:"available_devices"`
	AllocatedDevices  int            `json:"allocated_devices"`
	Devices           []*VideoDevice `json:"devices"`
	LastUpdated       time.Time      `json:"last_updated"`
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// HealthCheck represents the health status of the device plugin
type HealthCheck struct {
	Healthy       bool          `json:"healthy"`
	V4L2Healthy   bool          `json:"v4l2_healthy"`
	DevicesReady  bool          `json:"devices_ready"`
	KubeletConnected bool       `json:"kubelet_connected"`
	LastChecked   time.Time     `json:"last_checked"`
	Errors        []string      `json:"errors,omitempty"`
}
