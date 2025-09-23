package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// v4l2Manager implements the V4L2Manager interface
type v4l2Manager struct {
	devices map[string]*VideoDevice
	logger  *slog.Logger
	mu      sync.RWMutex
}

// NewV4L2Manager creates a new V4L2Manager instance
func NewV4L2Manager(logger *slog.Logger) V4L2Manager {
	return &v4l2Manager{
		devices: make(map[string]*VideoDevice),
		logger:  logger,
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

		// Set 0666 permissions on the device to ensure it's accessible
		if err := os.Chmod(devicePath, 0o666); err != nil {
			v.logger.Warn("Failed to set permissions", "device", devicePath, "error", err)
		} else {
			v.logger.Debug("Set permissions", "device", devicePath, "permissions", "0666")
		}

		// Create device entry
		device := &VideoDevice{
			ID:   deviceID,
			Path: devicePath,
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

	// Return a copy of the device (no allocation state tracking)
	return &VideoDevice{
		ID:   device.ID,
		Path: device.Path,
	}, nil
}

// IsHealthy checks if the V4L2 system is healthy
func (v *v4l2Manager) IsHealthy(maxDevices int) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	// If we have devices in our map, check them
	if len(v.devices) > 0 {
		// Check if all devices still exist and are accessible
		for _, device := range v.devices {
			if !checkDeviceExists(device.Path) || !checkDeviceReadable(device.Path) {
				v.logger.Warn("Device is not healthy", "device_id", device.ID, "device_path", device.Path)
				return false
			}
		}
		return true
	}

	// If no devices in our map, check if devices exist in the system
	// This handles the case where devices are created by startup script
	for i := 0; i < maxDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", VideoDeviceStartNumber+i)
		if !checkDeviceExists(devicePath) || !checkDeviceReadable(devicePath) {
			v.logger.Warn("System device is not healthy", "device_path", devicePath)
			return false
		}
	}

	return true
}

// GetDeviceCount returns the total number of devices
func (v *v4l2Manager) GetDeviceCount(maxDevices int) int {
	v.mu.RLock()
	defer v.mu.RUnlock()

	// If we have devices in our map, return that count
	if len(v.devices) > 0 {
		return len(v.devices)
	}

	// If no devices in our map, count devices in the system
	// This handles the case where devices are created by startup script
	count := 0
	for i := 0; i < maxDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", VideoDeviceStartNumber+i)
		if checkDeviceExists(devicePath) {
			count++
		}
	}
	return count
}

// ListAllDevices returns all devices
func (v *v4l2Manager) ListAllDevices() map[string]*VideoDevice {
	v.mu.RLock()
	defer v.mu.RUnlock()

	devices := make(map[string]*VideoDevice)
	for id, device := range v.devices {
		devices[id] = &VideoDevice{
			ID:   device.ID,
			Path: device.Path,
		}
	}

	return devices
}

// GetDeviceHealth returns health status for a specific device
func (v *v4l2Manager) GetDeviceHealth(deviceID string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	device, exists := v.devices[deviceID]
	if !exists {
		v.logger.Warn("Device not found for health check", "device_id", deviceID)
		return false
	}

	// Check if device exists and is readable
	healthy := checkDeviceExists(device.Path) && checkDeviceReadable(device.Path)
	if !healthy {
		v.logger.Warn("Device health check failed",
			"device_id", deviceID,
			"device_path", device.Path)
	}

	return healthy
}
