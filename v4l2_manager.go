package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// v4l2Manager implements the V4L2Manager interface
type v4l2Manager struct {
	devices map[string]*VideoDevice
	logger  *slog.Logger
	mu      sync.RWMutex
	healthy bool
}

// NewV4L2Manager creates a new V4L2Manager instance
func NewV4L2Manager(logger *slog.Logger) V4L2Manager {
	return &v4l2Manager{
		devices: make(map[string]*VideoDevice),
		logger:  logger,
		healthy: false,
	}
}

// CreateDevices creates the specified number of video devices
func (v *v4l2Manager) CreateDevices(count int) error {
	v.logger.Info("Creating video devices", "count", count)
	
	v.mu.Lock()
	defer v.mu.Unlock()
	
	// Clear existing devices
	v.devices = make(map[string]*VideoDevice)
	
	// Create devices from /dev/video{VideoDeviceStartNumber} to /dev/video{VideoDeviceStartNumber+count-1}
	// Starting from video{VideoDeviceStartNumber} to avoid conflicts with system video devices
	for i := 0; i < count; i++ {
		deviceID := fmt.Sprintf("video%d", VideoDeviceStartNumber+i)
		devicePath := fmt.Sprintf("/dev/video%d", VideoDeviceStartNumber+i)
		
		// Check if device exists
		if !checkDeviceExists(devicePath) {
			v.logger.Warn("Device does not exist", "device_path", devicePath)
			continue
		}
		
		// Check if device is readable
		if !checkDeviceReadable(devicePath) {
			v.logger.Warn("Device is not readable", "device_path", devicePath)
			continue
		}
		
		// Set 666 permissions on the device to ensure it's accessible
		if err := exec.Command("chmod", "666", devicePath).Run(); err != nil {
			v.logger.Warn("Failed to set permissions", "device", devicePath, "error", err)
		} else {
			v.logger.Debug("Set permissions", "device", devicePath, "permissions", "666")
		}
		
		// Create device entry
		device := &VideoDevice{
			ID:        deviceID,
			Path:      devicePath,
			Allocated: false,
		}
		
		v.devices[deviceID] = device
		v.logger.Debug("Created device", "device_id", deviceID, "device_path", devicePath)
	}
	
	actualCount := len(v.devices)
	if actualCount == 0 {
		return fmt.Errorf("no video devices were created")
	}
	
	if actualCount < count {
		v.logger.Warn("Created fewer devices than requested", 
			"requested", count, 
			"created", actualCount)
	}
	
	v.logger.Info("Successfully created video devices", 
		"requested", count, 
		"created", actualCount)
	
	v.healthy = true
	return nil
}

// GetAvailableDevices returns a list of unallocated devices
func (v *v4l2Manager) GetAvailableDevices() []*VideoDevice {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	var available []*VideoDevice
	for _, device := range v.devices {
		if !device.Allocated {
			available = append(available, device)
		}
	}
	
	return available
}

// AllocateDevice allocates a device to a pod
func (v *v4l2Manager) AllocateDevice() (*VideoDevice, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	
	// Find first available device
	for _, device := range v.devices {
		if !device.Allocated {
			device.Allocated = true
			device.AllocatedAt = time.Now()
			
			v.logger.Info("Device allocated", 
				"device_id", device.ID,
				"device_path", device.Path)
			
			return device, nil
		}
	}
	
	return nil, fmt.Errorf("no available devices")
}

// ReleaseDevice releases a device back to the pool
func (v *v4l2Manager) ReleaseDevice(deviceID string) error {
	if deviceID == "" {
		return fmt.Errorf("device ID cannot be empty")
	}
	
	v.mu.Lock()
	defer v.mu.Unlock()
	
	device, exists := v.devices[deviceID]
	if !exists {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	
	if !device.Allocated {
		v.logger.Warn("Device is not allocated", "device_id", deviceID)
		return nil
	}
	
	device.Allocated = false
	device.AllocatedAt = time.Time{}
	
	v.logger.Info("Device released", 
		"device_id", deviceID)
	
	return nil
}

// IsHealthy checks if the V4L2 system is healthy
func (v *v4l2Manager) IsHealthy() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	// If we have devices in our map, check them
	if len(v.devices) > 0 {
		// Check if all devices still exist and are accessible
		for _, device := range v.devices {
			if !checkDeviceExists(device.Path) || !checkDeviceReadable(device.Path) {
				v.logger.Warn("Device is not healthy", "device_id", device.ID)
				return false
			}
		}
		return true
	}
	
	// If no devices in our map, check if devices exist in the system
	// This handles the case where devices are created by startup script
	for i := 0; i < 8; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", i)
		if !checkDeviceExists(devicePath) || !checkDeviceReadable(devicePath) {
			return false
		}
	}
	
	return true
}

// GetDeviceCount returns the total number of devices
func (v *v4l2Manager) GetDeviceCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	// If we have devices in our map, return that count
	if len(v.devices) > 0 {
		return len(v.devices)
	}
	
	// If no devices in our map, count devices in the system
	// This handles the case where devices are created by startup script
	count := 0
	for i := 0; i < 8; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", i)
		if checkDeviceExists(devicePath) {
			count++
		}
	}
	return count
}

// GetAllocatedDeviceCount returns the number of allocated devices
func (v *v4l2Manager) GetAllocatedDeviceCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	count := 0
	for _, device := range v.devices {
		if device.Allocated {
			count++
		}
	}
	return count
}

// isModuleLoaded checks if the v4l2loopback module is loaded
func (v *v4l2Manager) isModuleLoaded() bool {
	cmd := exec.Command("lsmod")
	output, err := cmd.Output()
	if err != nil {
		v.logger.Debug("Failed to check loaded modules", "error", err)
		return false
	}
	
	return strings.Contains(string(output), "v4l2loopback")
}

// GetDeviceStatus returns the current status of all devices
func (v *v4l2Manager) GetDeviceStatus() *DeviceStatus {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	devices := make([]*VideoDevice, 0, len(v.devices))
	allocatedCount := 0
	
	for _, device := range v.devices {
		// Create a copy to avoid race conditions
		deviceCopy := &VideoDevice{
			ID:          device.ID,
			Path:        device.Path,
			Allocated:   device.Allocated,
			AllocatedAt: device.AllocatedAt,
		}
		devices = append(devices, deviceCopy)
		
		if device.Allocated {
			allocatedCount++
		}
	}
	
	return &DeviceStatus{
		TotalDevices:     len(v.devices),
		AvailableDevices: len(v.devices) - allocatedCount,
		AllocatedDevices: allocatedCount,
		Devices:          devices,
		LastUpdated:      time.Now(),
	}
}

// ValidateDevices validates that all devices are working correctly
func (v *v4l2Manager) ValidateDevices() error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	var errors []string
	
	for _, device := range v.devices {
		if !checkDeviceExists(device.Path) {
			errors = append(errors, fmt.Sprintf("device %s does not exist", device.Path))
			continue
		}
		
		if !checkDeviceReadable(device.Path) {
			errors = append(errors, fmt.Sprintf("device %s is not readable", device.Path))
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("device validation failed: %s", strings.Join(errors, "; "))
	}
	
	return nil
}

// GetDeviceByID returns a device by its ID
func (v *v4l2Manager) GetDeviceByID(deviceID string) (*VideoDevice, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	device, exists := v.devices[deviceID]
	if !exists {
		return nil, fmt.Errorf("device not found: %s", deviceID)
	}
	
	// Return a copy to avoid race conditions
	return &VideoDevice{
		ID:          device.ID,
		Path:        device.Path,
		Allocated:   device.Allocated,
		AllocatedAt: device.AllocatedAt,
	}, nil
}

// MarkDeviceAsAllocated marks a device as allocated (used during startup reconciliation)
func (v *v4l2Manager) MarkDeviceAsAllocated(deviceID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	
	device, exists := v.devices[deviceID]
	if !exists {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	
	if device.Allocated {
		v.logger.Warn("Device already allocated", "device_id", deviceID)
		return nil
	}
	
	device.Allocated = true
	device.AllocatedAt = time.Now()
	
	v.logger.Info("Marked device as allocated", "device_id", deviceID)
	return nil
}

// ListAllDevices returns all devices (for debugging)
func (v *v4l2Manager) ListAllDevices() map[string]*VideoDevice {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	devices := make(map[string]*VideoDevice)
	for id, device := range v.devices {
		devices[id] = &VideoDevice{
			ID:          device.ID,
			Path:        device.Path,
			Allocated:   device.Allocated,
			AllocatedAt: device.AllocatedAt,
		}
	}
	
	return devices
}
