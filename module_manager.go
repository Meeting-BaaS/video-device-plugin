package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// loadV4L2LoopbackModule loads the v4l2loopback kernel module
func loadV4L2LoopbackModule(config *DevicePluginConfig, logger *slog.Logger) error {
	logger.Info("Loading v4l2loopback kernel module...")

	// Check if module is already loaded and verify configuration
	if loaded, err := isModuleLoaded("v4l2loopback"); err == nil && loaded {
		logger.Info("v4l2loopback module already loaded, verifying configuration...")

		// Check if the current device configuration matches our requirements
		if err := verifyV4L2Configuration(config, logger); err != nil {
			logger.Warn("v4l2loopback configuration mismatch detected", "error", err)
			logger.Info("Reloading v4l2loopback module with correct configuration...")

			// Unload the module first (time-bounded)
			unloadCtx, unloadCancel := context.WithTimeout(context.Background(), time.Duration(config.DeviceCreationTimeout)*time.Second)
			defer unloadCancel()
			if unloadErr := exec.CommandContext(unloadCtx, "modprobe", "-r", "v4l2loopback").Run(); unloadErr != nil {
				logger.Warn("Failed to unload existing v4l2loopback module", "error", unloadErr)
				// Continue anyway, modprobe might handle the reload
			}
		} else {
			logger.Info("v4l2loopback configuration matches requirements")
			return nil
		}
	}

	// CRITICAL: Load videodev module first (required for v4l2loopback)
	logger.Info("Loading videodev module (required for v4l2loopback)...")
	vctx, vcancel := context.WithTimeout(context.Background(), time.Duration(config.DeviceCreationTimeout)*time.Second)
	defer vcancel()
	if out, err := exec.CommandContext(vctx, "modprobe", "videodev").CombinedOutput(); err != nil {
		logger.Error("Failed to load videodev module - this is required for v4l2loopback", "error", err, "output", strings.TrimSpace(string(out)))
		logger.Info("Make sure linux-modules-extra-$(uname -r) is installed")
		return fmt.Errorf("failed to load videodev module: %w", err)
	}

	// Verify videodev is loaded
	if loaded, err := isModuleLoaded("videodev"); err != nil {
		logger.Error("Failed to check videodev module status", "error", err)
		return fmt.Errorf("failed to check videodev module: %w", err)
	} else if !loaded {
		logger.Error("ERROR: videodev module is not loaded - v4l2loopback will fail")
		return fmt.Errorf("videodev module not loaded")
	} else {
		logger.Info("videodev module loaded successfully")
	}

	// Load the v4l2loopback module with our specific parameters
	// Using video_nr=VideoDeviceStartNumber-{VideoDeviceStartNumber+max_devices-1} to avoid conflicts with system video devices
	videoNumbers := make([]string, config.MaxDevices)
	cardLabels := make([]string, config.MaxDevices)
	exclusiveCaps := make([]string, config.MaxDevices)
	for i := 0; i < config.MaxDevices; i++ {
		videoNumbers[i] = fmt.Sprintf("%d", VideoDeviceStartNumber+i)
		cardLabels[i] = fmt.Sprintf(`"%s"`, config.V4L2CardLabel)
		exclusiveCaps[i] = fmt.Sprintf("%d", config.V4L2ExclusiveCaps)
	}

	// Create context with timeout for modprobe command
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.DeviceCreationTimeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "modprobe", "v4l2loopback",
		fmt.Sprintf("video_nr=%s", strings.Join(videoNumbers, ",")),
		fmt.Sprintf("max_buffers=%d", config.V4L2MaxBuffers),
		fmt.Sprintf("exclusive_caps=%s", strings.Join(exclusiveCaps, ",")),
		fmt.Sprintf("card_label=%s", strings.Join(cardLabels, ",")))

	if out, err := cmd.CombinedOutput(); err != nil {
		// Check if the error is due to timeout
		if ctx.Err() == context.DeadlineExceeded {
			logger.Error("Failed to load v4l2loopback module - operation timed out",
				"timeout_seconds", config.DeviceCreationTimeout)
			return fmt.Errorf("modprobe timed out after %d seconds: %w", config.DeviceCreationTimeout, err)
		}

		logger.Error("Failed to load v4l2loopback module")
		logger.Info("modprobe output", "output", strings.TrimSpace(string(out)))

		// dmesg fallback for additional debugging
		logger.Info("Checking dmesg for additional error details:")
		if dmesgOutput, dmesgErr := exec.Command("dmesg").Output(); dmesgErr == nil {
			lines := strings.Split(string(dmesgOutput), "\n")
			for i := len(lines) - 10; i < len(lines); i++ {
				if i >= 0 {
					logger.Info("   " + lines[i])
				}
			}
		} else {
			logger.Debug("dmesg not available or restricted", "error", dmesgErr)
		}
		return fmt.Errorf("failed to load v4l2loopback module: %w", err)
	}

	logger.Info("v4l2loopback module loaded successfully")
	return nil
}

// cleanupV4L2Module unloads the v4l2loopback module on shutdown
func cleanupV4L2Module(config *DevicePluginConfig, logger *slog.Logger) {
	logger.Info("Cleaning up v4l2loopback module")

	// Check if v4l2loopback module is loaded
	cmd := exec.Command("lsmod")
	output, err := cmd.Output()
	if err != nil {
		logger.Warn("Failed to check loaded modules", "error", err)
		return
	}

	if !strings.Contains(string(output), "v4l2loopback") {
		logger.Info("v4l2loopback module not loaded, nothing to cleanup")
		return
	}

	// Unload v4l2loopback module
	logger.Info("Unloading v4l2loopback module...")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.CleanupTimeout)*time.Second)
	defer cancel()
	unloadCmd := exec.CommandContext(ctx, "modprobe", "-r", "v4l2loopback")
	if out, err := unloadCmd.CombinedOutput(); err != nil {
		logger.Warn("Failed to unload v4l2loopback module", "error", err, "output", strings.TrimSpace(string(out)))
		logger.Info("Module may be in use by other processes")
	} else {
		logger.Info("v4l2loopback module unloaded successfully")
	}

	// Check if videodev module can be unloaded (if not needed by other modules)
	if loaded, err := isModuleLoaded("videodev"); err == nil && loaded {
		logger.Info("Checking if videodev module can be unloaded")
		// Check if any other video modules are using videodev
		if loaded, err := isModuleLoaded("v4l2loopback"); err == nil && !loaded {
			// No other modules using videodev, try to unload it
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.CleanupTimeout)*time.Second)
			defer cancel()
			unloadVideodevCmd := exec.CommandContext(ctx, "modprobe", "-r", "videodev")
			if out, err := unloadVideodevCmd.CombinedOutput(); err != nil {
				logger.Info("videodev module still needed by other modules, keeping loaded", "output", strings.TrimSpace(string(out)))
			} else {
				logger.Info("videodev module unloaded successfully")
			}
		}
	}

	logger.Info("Cleanup completed")
}

// isModuleLoaded checks if a specific kernel module is loaded by parsing lsmod output
func isModuleLoaded(moduleName string) (bool, error) {
	lsmodOutput, err := exec.Command("lsmod").Output()
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(string(lsmodOutput), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == moduleName {
			return true, nil
		}
	}

	return false, nil
}

// verifyV4L2Configuration checks if the current v4l2loopback configuration matches requirements
func verifyV4L2Configuration(config *DevicePluginConfig, logger *slog.Logger) error {
	// Check if the expected number of devices exist
	expectedDevices := config.MaxDevices
	actualDevices := 0

	for i := VideoDeviceStartNumber; i < VideoDeviceStartNumber+expectedDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", i)
		if _, err := os.Stat(devicePath); err == nil {
			actualDevices++
		}
	}

	logger.Info("v4l2loopback configuration check",
		"expected_devices", expectedDevices,
		"actual_devices", actualDevices,
		"device_range", fmt.Sprintf("/dev/video%d-%d", VideoDeviceStartNumber, VideoDeviceStartNumber+expectedDevices-1))

	if actualDevices != expectedDevices {
		return fmt.Errorf("device count mismatch: expected %d devices, found %d", expectedDevices, actualDevices)
	}

	// Check if devices are character devices and have correct permissions
	for i := VideoDeviceStartNumber; i < VideoDeviceStartNumber+expectedDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", i)
		if stat, err := os.Stat(devicePath); err == nil {
			// Check if it's a character device
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				return fmt.Errorf("device %s is not a character device", devicePath)
			}

			// Check permissions (optional - just log for debugging)
			expectedPerm := os.FileMode(config.V4L2DevicePerm)
			if stat.Mode().Perm() != expectedPerm.Perm() {
				logger.Debug("device permission mismatch",
					"device", devicePath,
					"expected", fmt.Sprintf("%o", expectedPerm.Perm()),
					"actual", fmt.Sprintf("%o", stat.Mode().Perm()))
			}
		} else {
			return fmt.Errorf("device %s not found: %w", devicePath, err)
		}
	}

	return nil
}
