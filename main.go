package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	fmt.Println("🚀 Starting Video Device Plugin Container")
	fmt.Println("==========================================")
	
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
	
	// Check if running as root
	if err := checkRoot(); err != nil {
		logger.Error("Root check failed", "error", err)
		os.Exit(1)
	}
	
	// Display system information
	displaySystemInfo(logger)
	
	// Load v4l2loopback module
	if err := loadV4L2LoopbackModule(logger); err != nil {
		logger.Error("Failed to load v4l2loopback module", "error", err)
		os.Exit(1)
	}
	
	// Verify devices were created
	if err := verifyVideoDevices(logger); err != nil {
		logger.Error("Failed to verify video devices", "error", err)
		os.Exit(1)
	}
	
	// Set device permissions
	if err := setDevicePermissions(logger); err != nil {
		logger.Error("Failed to set device permissions", "error", err)
		os.Exit(1)
	}
	
	// Initialize V4L2 manager
	v4l2Manager := NewV4L2Manager(logger)
	
	// Initialize device plugin
	plugin := NewVideoDevicePlugin(config, v4l2Manager, logger)
	
	// Set up signal handling for graceful shutdown
	sigChan := setupSignalHandling()
	
	// Start the device plugin in a goroutine
	go func() {
		if err := plugin.Start(); err != nil {
			logger.Error("Failed to start device plugin", "error", err)
			os.Exit(1)
		}
	}()
	
	// Wait for devices to be ready
	if err := waitForDevicesReady(v4l2Manager, logger); err != nil {
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
func waitForDevicesReady(v4l2Manager V4L2Manager, logger *slog.Logger) error {
	logger.Info("🎯 Starting video device plugin...")
	
	// Devices are already created by the main function, just verify they exist
	// Wait for devices to be available
	maxWait := 30 * time.Second
	checkInterval := 1 * time.Second
	start := time.Now()
	
	for time.Since(start) < maxWait {
		// Check if devices are healthy and available
		if v4l2Manager.IsHealthy() && v4l2Manager.GetDeviceCount() > 0 {
			logger.Info("Devices are ready", 
				"device_count", v4l2Manager.GetDeviceCount(),
				"available_devices", len(v4l2Manager.GetAvailableDevices()))
			return nil
		}
		
		logger.Debug("Waiting for devices to be ready...")
		time.Sleep(checkInterval)
	}
	
	return fmt.Errorf("devices not ready after %v", maxWait)
}


// setupGracefulShutdown sets up graceful shutdown handling
func setupGracefulShutdown(plugin *VideoDevicePlugin, logger *slog.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		sig := <-sigChan
		logger.Info("Received shutdown signal", "signal", sig.String())
		
		// Stop the plugin
		if err := plugin.Stop(); err != nil {
			logger.Error("Error during graceful shutdown", "error", err)
		}
		
		// Exit
		os.Exit(0)
	}()
}

// logStartupInfo logs startup information
func logStartupInfo(config *DevicePluginConfig, logger *slog.Logger) {
	logger.Info("Video Device Plugin Configuration",
		"max_devices", config.MaxDevices,
		"node_name", config.NodeName,
		"kubelet_socket", config.KubeletSocket,
		"resource_name", config.ResourceName,
		"socket_path", config.SocketPath,
		"log_level", config.LogLevel)
}

// validateEnvironment validates the runtime environment
func validateEnvironment(logger *slog.Logger) error {
	// Check if running as root (required for device access)
	if os.Geteuid() != 0 {
		logger.Warn("Not running as root - device access may be limited")
	}
	
	// Check if /dev directory exists
	if _, err := os.Stat("/dev"); err != nil {
		return fmt.Errorf("/dev directory not accessible: %w", err)
	}
	
	// Check if modprobe is available
	if _, err := os.Stat("/sbin/modprobe"); err != nil {
		logger.Warn("modprobe not found - module loading may fail")
	}
	
	return nil
}

// printBanner prints the application banner
func printBanner() {
	fmt.Println(`
╔══════════════════════════════════════════════════════════════╗
║                    Video Device Plugin                      ║
║              Kubernetes Device Plugin for v4l2loopback      ║
║                                                              ║
║  Manages virtual video devices for meeting bot pods         ║
║  Resource: meeting-baas.io/video-devices                    ║
╚══════════════════════════════════════════════════════════════╝
`)
}
