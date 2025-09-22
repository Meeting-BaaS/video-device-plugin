package main

import (
	"fmt"
	"log/slog"
	"os"
)

// verifyVideoDevices verifies that video devices were created
func verifyVideoDevices(config *DevicePluginConfig, logger *slog.Logger) error {
	logger.Info("Verifying video devices...")
	
	deviceCount := 0
	for i := VideoDeviceStartNumber; i < VideoDeviceStartNumber+config.MaxDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", i)
		if _, err := os.Stat(devicePath); err == nil {
			deviceCount++
			if stat, err := os.Stat(devicePath); err == nil {
				logger.Info(fmt.Sprintf("   %s %s %s %s %s %s", 
					stat.Mode().String(),
					"1", "root", "video", 
					fmt.Sprintf("%d, %d", 81, i-VideoDeviceStartNumber),
					stat.ModTime().Format("Jan 02 15:04"),
					devicePath))
			}
		}
	}
	
	if deviceCount == 0 {
		return fmt.Errorf("no video devices found")
	}
	
	logger.Info(fmt.Sprintf("Found %d video devices:", deviceCount))
	return nil
}

// setDevicePermissions sets proper permissions on video devices
func setDevicePermissions(config *DevicePluginConfig, logger *slog.Logger) error {
	logger.Info("Setting device permissions...")
	
	for i := VideoDeviceStartNumber; i < VideoDeviceStartNumber+config.MaxDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", i)
		if _, err := os.Stat(devicePath); err == nil {
			// Set permissions to 666 (rw-rw-rw-)
			if err := os.Chmod(devicePath, 0666); err != nil {
				logger.Warn("Failed to set permissions", "device", devicePath, "error", err)
			}
		}
	}
	
	logger.Info("Device permissions set")
	return nil
}
