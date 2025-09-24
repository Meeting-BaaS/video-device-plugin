package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
	perm    os.FileMode
}

// NewV4L2Manager creates a new V4L2Manager instance
func NewV4L2Manager(logger *slog.Logger, devicePerm int) V4L2Manager {
	return &v4l2Manager{
		devices: make(map[string]*VideoDevice),
		logger:  logger,
		perm:    os.FileMode(devicePerm),
	}
}

// CreateDevices discovers and registers the specified number of video devices
func (v *v4l2Manager) CreateDevices(count int) error {
	v.logger.Info("Discovering video devices", "count", count)

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

		// Set configured permissions on the device
		if err := os.Chmod(devicePath, v.perm); err != nil {
			v.logger.Warn("Failed to set permissions", "device", devicePath, "error", err)
		} else {
			v.logger.Debug("Set permissions", "device", devicePath, "permissions", fmt.Sprintf("%#o", v.perm))
		}

		// Create device entry
		device := &VideoDevice{
			ID:   deviceID,
			Path: devicePath,
		}

		v.devices[deviceID] = device
		v.logger.Debug("Registered device", "device_id", deviceID, "device_path", devicePath)
	}

	actualCount := len(v.devices)
	if actualCount == 0 {
		return fmt.Errorf("no video devices were found")
	}

	if actualCount < count {
		v.logger.Warn("Found fewer devices than requested",
			"requested", count,
			"found", actualCount)
	}

	v.logger.Info("Successfully registered video devices",
		"requested", count,
		"registered", actualCount)

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

// HasVideoCaptureCapability checks if a device has Video Capture capability
func (v *v4l2Manager) HasVideoCaptureCapability(devicePath string, timeoutSeconds int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "v4l2-ctl", "--device", devicePath, "--info")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			v.logger.Error("Video capability check timed out", "device", devicePath, "timeout_seconds", timeoutSeconds)
		} else {
			v.logger.Error("Failed to check device capabilities", "device", devicePath, "error", err)
		}
		return false
	}

	return strings.Contains(string(output), "Video Capture")
}

// CheckAllDevicesCapabilities checks all devices for Video Capture capability
// Returns a list of devices that are missing the capability
func (v *v4l2Manager) CheckAllDevicesCapabilities(maxDevices int, timeoutSeconds int) []string {
	var stuckDevices []string

	for i := 0; i < maxDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", VideoDeviceStartNumber+i)
		if !v.HasVideoCaptureCapability(devicePath, timeoutSeconds) {
			stuckDevices = append(stuckDevices, devicePath)
		}
	}

	return stuckDevices
}
