package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"
)

func main() {
	// Load configuration
	config := loadConfig()

	// Validate configuration
	if err := validateConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Initialize structured logging
	logger := setupLogger(config.LogLevel)
	logger.Info("Starting Video Device Plugin initialization...")

	// Debug: Show loaded configuration
	if config.Debug {
		logger.Info("Configuration loaded",
			"max_devices", config.MaxDevices,
			"node_name", config.NodeName,
			"log_level", config.LogLevel,
			"debug", config.Debug,
			"v4l2_card_label", config.V4L2CardLabel,
			"v4l2_max_buffers", config.V4L2MaxBuffers,
			"v4l2_exclusive_caps", config.V4L2ExclusiveCaps,
			"resource_name", config.ResourceName,
			"kubelet_socket", config.KubeletSocket,
			"socket_path", config.SocketPath)
	}

	// Warn about v4l2loopback device limit
	if config.MaxDevices == 8 {
		logger.Info("Using maximum device count", "max_devices", config.MaxDevices, "note", "v4l2loopback supports maximum 8 devices")
	}

	// Check if running as root
	if err := checkRoot(logger); err != nil {
		logger.Error("Root check failed", "error", err)
		os.Exit(1)
	}

	// Display system information
	displaySystemInfo(logger)

	// Load v4l2loopback module
	if err := loadV4L2LoopbackModule(config, logger); err != nil {
		logger.Error("Failed to load v4l2loopback module", "error", err)
		os.Exit(1)
	}

	// Verify devices were created
	if err := verifyVideoDevices(config, logger); err != nil {
		logger.Error("Failed to verify video devices", "error", err)
		os.Exit(1)
	}

	// Ensure device count and types match config exactly
	if err := verifyV4L2Configuration(config, logger); err != nil {
		logger.Error("v4l2 configuration verification failed", "error", err)
		os.Exit(1)
	}

	// Initialize V4L2 manager
	v4l2Manager := NewV4L2Manager(logger, config.V4L2DevicePerm)

	// Populate the V4L2 manager with the devices we just created
	if err := v4l2Manager.CreateDevices(config.MaxDevices); err != nil {
		logger.Error("Failed to populate V4L2 manager with devices", "error", err)
		os.Exit(1)
	}

	// Initialize device plugin
	plugin := NewVideoDevicePlugin(config, v4l2Manager, logger)

	// Set up signal handling for graceful shutdown
	sigChan := setupSignalHandling()

	// Start the device plugin in a goroutine
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- plugin.Start()
	}()

	// Wait for plugin to start or fail
	if err := <-startErrCh; err != nil {
		logger.Error("Failed to start device plugin", "error", err)
		os.Exit(1)
	}

	// Wait for devices to be ready
	if err := waitForDevicesReady(v4l2Manager, config, logger); err != nil {
		logger.Error("Devices not ready", "error", err)
		os.Exit(1)
	}

	logger.Info("Video device plugin is ready and running")

	// Wait for shutdown signal
	waitForSignal(sigChan, logger)

	// Graceful shutdown
	logger.Info("Shutting down video device plugin")
	if err := plugin.Stop(); err != nil {
		logger.Error("Error during shutdown", "error", err)
	}

	// Cleanup v4l2loopback module
	cleanupV4L2Module(logger)

	logger.Info("Video device plugin shutdown complete")
}

// waitForDevicesReady waits for devices to be created and ready
func waitForDevicesReady(v4l2Manager V4L2Manager, config *DevicePluginConfig, logger *slog.Logger) error {
	logger.Info("Waiting for devices to be ready...")

	// Wait for devices to be available
	maxWait := time.Duration(config.DeviceCreationTimeout) * time.Second
	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}
	checkInterval := 1 * time.Second
	start := time.Now()

	for time.Since(start) < maxWait {
		// Check if devices are healthy
		if v4l2Manager.IsHealthy(config.MaxDevices) && v4l2Manager.GetDeviceCount(config.MaxDevices) > 0 {
			logger.Info("Devices are ready",
				"device_count", v4l2Manager.GetDeviceCount(config.MaxDevices))
			return nil
		}

		logger.Debug("Waiting for devices to be ready...")
		time.Sleep(checkInterval)
	}

	return fmt.Errorf("devices not ready after %v", maxWait)
}
