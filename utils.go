package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
)

// Constants for video device management
const (
	// VideoDeviceStartNumber is the first video device number to use
	// Starting from 10 to avoid conflicts with system video devices (video0-9)
	VideoDeviceStartNumber = 10
)

// setupLogger creates and configures a structured logger
func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler)
}

// loadConfig loads configuration from environment variables
func loadConfig() *DevicePluginConfig {
	// Try to load .env file if it exists
	_ = loadEnvFile() // .env file not found or error loading it - that's okay, continue with system env vars

	config := &DevicePluginConfig{
		// Core Configuration
		MaxDevices:    getEnvInt("MAX_DEVICES", 8),
		NodeName:      getEnv("NODE_NAME", ""),
		KubeletSocket: getEnv("KUBELET_SOCKET", "/var/lib/kubelet/device-plugins/kubelet.sock"),
		ResourceName:  getEnv("RESOURCE_NAME", "meeting-baas.io/video-devices"),
		SocketPath:    getEnv("SOCKET_PATH", "/var/lib/kubelet/device-plugins/video-device-plugin.sock"),
		LogLevel:      getEnv("LOG_LEVEL", "info"),

		// Development/Debugging
		Debug: getEnvBool("DEBUG", false),

		// V4L2 Configuration
		V4L2MaxBuffers:    getEnvInt("V4L2_MAX_BUFFERS", 2),
		V4L2ExclusiveCaps: getEnvInt("V4L2_EXCLUSIVE_CAPS", 1),
		V4L2CardLabel:     getEnv("V4L2_CARD_LABEL", "Default WebCam"),

		// Kubernetes Integration
		KubernetesNamespace: getEnv("KUBERNETES_NAMESPACE", "kube-system"),
		ServiceAccountName:  getEnv("SERVICE_ACCOUNT_NAME", "video-device-plugin"),

		// Monitoring and Observability
		EnableMetrics:       getEnvBool("ENABLE_METRICS", false),
		MetricsPort:         getEnvInt("METRICS_PORT", 8080),
		HealthCheckInterval: getEnvInt("HEALTH_CHECK_INTERVAL", 30),

		// Performance Tuning
		AllocationTimeout:     getEnvInt("ALLOCATION_TIMEOUT", 30),
		DeviceCreationTimeout: getEnvInt("DEVICE_CREATION_TIMEOUT", 60),
		ShutdownTimeout:       getEnvInt("SHUTDOWN_TIMEOUT", 10),
	}

	// Validate MaxDevices - v4l2loopback has a hard limit of 8 devices
	if config.MaxDevices > 8 {
		config.MaxDevices = 8
	}
	if config.MaxDevices < 1 {
		config.MaxDevices = 1
	}

	return config
}

// loadEnvFile loads environment variables from .env file
func loadEnvFile() error {
	// Check if .env file exists
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		return fmt.Errorf(".env file not found")
	}

	// Load .env file
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("error loading .env file: %w", err)
	}

	return nil
}

// validateConfig validates the configuration
func validateConfig(config *DevicePluginConfig) error {
	if config.MaxDevices <= 0 || config.MaxDevices > 8 {
		return fmt.Errorf("MAX_DEVICES must be between 1 and 8, got %d", config.MaxDevices)
	}

	if config.NodeName == "" {
		return fmt.Errorf("NODE_NAME is required")
	}

	if config.ResourceName == "" {
		return fmt.Errorf("RESOURCE_NAME is required")
	}

	if config.SocketPath == "" {
		return fmt.Errorf("SOCKET_PATH is required")
	}

	return nil
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an environment variable as an integer with a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvBool gets an environment variable as a boolean with a default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// checkDeviceExists checks if a device file exists and is accessible
func checkDeviceExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// checkDeviceReadable checks if a device file is readable
func checkDeviceReadable(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() {
		_ = file.Close() // Ignore close error for device readability check
	}()
	return true
}

// setupSignalHandling sets up signal handling for graceful shutdown
func setupSignalHandling() <-chan os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	return sigChan
}

// waitForSignal waits for a shutdown signal
func waitForSignal(sigChan <-chan os.Signal, logger *slog.Logger) {
	sig := <-sigChan
	logger.Info("Received shutdown signal", "signal", sig.String())
}

// ensureDirectory ensures a directory exists
func ensureDirectory(path string) error {
	return os.MkdirAll(path, 0755)
}

// cleanupSocket removes the device plugin socket file
func cleanupSocket(socketPath string) error {
	if _, err := os.Stat(socketPath); err == nil {
		return os.Remove(socketPath)
	}
	return nil
}
