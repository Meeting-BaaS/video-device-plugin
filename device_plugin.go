package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// VideoDevicePlugin implements the Kubernetes device plugin gRPC server
type VideoDevicePlugin struct {
	pluginapi.UnimplementedDevicePluginServer
	config      *DevicePluginConfig
	v4l2Manager V4L2Manager
	logger      *slog.Logger
	server      *grpc.Server
	stopCh      chan struct{}
	mu          sync.RWMutex
	registered  bool
}

// NewVideoDevicePlugin creates a new VideoDevicePlugin instance
func NewVideoDevicePlugin(config *DevicePluginConfig, v4l2Manager V4L2Manager, logger *slog.Logger) *VideoDevicePlugin {
	plugin := &VideoDevicePlugin{
		config:      config,
		v4l2Manager: v4l2Manager,
		logger:      logger,
		stopCh:      make(chan struct{}),
		registered:  false,
	}

	return plugin
}

// Start starts the device plugin server
func (p *VideoDevicePlugin) Start() error {
	p.logger.Info("Starting video device plugin",
		"resource_name", p.config.ResourceName,
		"socket_path", p.config.SocketPath)

	// Ensure socket directory exists
	if err := ensureDirectory(filepath.Dir(p.config.SocketPath)); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Clean up any existing socket
	if err := cleanupSocket(p.config.SocketPath); err != nil {
		p.logger.Warn("Failed to cleanup existing socket", "error", err)
	}

	// Create gRPC server
	p.server = grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(p.server, p)

	// Start gRPC server
	listener, err := net.Listen("unix", p.config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Start server in goroutine
	serverReady := make(chan struct{})
	go func() {
		p.logger.Info("Starting gRPC server", "socket", p.config.SocketPath)
		// Signal that server is ready to accept connections
		close(serverReady)
		if err := p.server.Serve(listener); err != nil {
			p.logger.Error("gRPC server failed", "error", err)
		}
	}()

	// Wait for server to be ready
	select {
	case <-serverReady:
		// Server started successfully
	case <-time.After(5 * time.Second):
		return fmt.Errorf("gRPC server failed to start within timeout")
	}

	// Register with kubelet
	if err := p.RegisterWithKubelet(); err != nil {
		// Cleanup to avoid leaving a dangling socket
		if p.server != nil {
			p.server.Stop()
		}
		_ = cleanupSocket(p.config.SocketPath)
		return fmt.Errorf("failed to register with kubelet: %w", err)
	}

	// Start kubelet restart monitoring
	go p.monitorKubeletRestart()

	p.logger.Info("Video device plugin started successfully")
	return nil
}

// Stop stops the device plugin server
func (p *VideoDevicePlugin) Stop() error {
	p.logger.Info("Stopping video device plugin")

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.server != nil {
		p.server.Stop()
	}

	// Clean up socket
	if err := cleanupSocket(p.config.SocketPath); err != nil {
		p.logger.Warn("Failed to cleanup socket", "error", err)
	}

	close(p.stopCh)
	p.logger.Info("Video device plugin stopped")
	return nil
}

// WaitForShutdown waits for shutdown signal
func (p *VideoDevicePlugin) WaitForShutdown() {
	<-p.stopCh
}

// RegisterWithKubelet registers the device plugin with kubelet
func (p *VideoDevicePlugin) RegisterWithKubelet() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.registered {
		return nil
	}

	p.logger.Info("Registering with kubelet",
		"resource_name", p.config.ResourceName,
		"kubelet_socket", p.config.KubeletSocket)

	// Connect to kubelet socket (Unix domain socket)
	conn, err := grpc.NewClient("unix://"+p.config.KubeletSocket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to kubelet: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			p.logger.Warn("Failed to close kubelet connection", "error", closeErr)
		}
	}()

	// Create registration client
	client := pluginapi.NewRegistrationClient(conn)

	// Create registration request
	req := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     filepath.Base(p.config.SocketPath),
		ResourceName: p.config.ResourceName,
	}

	// Send registration request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Register(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to register with kubelet: %w", err)
	}

	p.registered = true
	p.logger.Info("Successfully registered with kubelet")
	return nil
}

// ListAndWatch implements the ListAndWatch gRPC method
func (p *VideoDevicePlugin) ListAndWatch(req *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	p.logger.Debug("ListAndWatch called")

	// Get all devices (always report all available devices)
	allDevices := p.v4l2Manager.ListAllDevices()

	var devices []*pluginapi.Device
	healthyCount := 0
	for _, device := range allDevices {
		// Check health of each device individually
		deviceHealthy := p.v4l2Manager.GetDeviceHealth(device.ID)
		if deviceHealthy {
			healthyCount++
		}

		health := pluginapi.Healthy
		if !deviceHealthy {
			health = pluginapi.Unhealthy
		}

		devices = append(devices, &pluginapi.Device{
			ID:     device.ID,
			Health: health,
		})
	}

	p.logger.Info("Found video devices",
		"device_count", len(devices),
		"healthy_count", healthyCount,
		"unhealthy_count", len(devices)-healthyCount)

	// Send initial device list
	response := &pluginapi.ListAndWatchResponse{
		Devices: devices,
	}
	if err := stream.Send(response); err != nil {
		return err
	}

	// Simple health monitoring loop (like GPU plugin)
	ticker := time.NewTicker(time.Duration(p.config.HealthCheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			p.logger.Debug("ListAndWatch stopping")
			return nil
		case <-ticker.C:
			// Periodic health check - send updated device list with per-device health status
			allDevices := p.v4l2Manager.ListAllDevices()

			var devices []*pluginapi.Device
			healthyCount := 0
			for _, device := range allDevices {
				// Check health of each device individually
				deviceHealthy := p.v4l2Manager.GetDeviceHealth(device.ID)
				if deviceHealthy {
					healthyCount++
				}

				health := pluginapi.Healthy
				if !deviceHealthy {
					health = pluginapi.Unhealthy
				}

				devices = append(devices, &pluginapi.Device{
					ID:     device.ID,
					Health: health,
				})
			}

			p.logger.Debug("Health check completed",
				"device_count", len(devices),
				"healthy_count", healthyCount,
				"unhealthy_count", len(devices)-healthyCount)

			response := &pluginapi.ListAndWatchResponse{
				Devices: devices,
			}
			if err := stream.Send(response); err != nil {
				p.logger.Error("Failed to send device list", "error", err)
				return err
			}
		}
	}
}

// Allocate implements the Allocate gRPC method
func (p *VideoDevicePlugin) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	p.logger.Info("Allocate called", "requests", len(req.ContainerRequests))

	var responses []*pluginapi.ContainerAllocateResponse

	for i, containerReq := range req.ContainerRequests {
		p.logger.Debug("Processing container request",
			"container_index", i,
			"device_ids", containerReq.DevicesIDs)

		response, err := p.allocateContainer(containerReq)
		if err != nil {
			p.logger.Error("Failed to allocate container", "error", err)
			return nil, err
		}
		responses = append(responses, response)
	}

	finalResponse := &pluginapi.AllocateResponse{
		ContainerResponses: responses,
	}

	p.logger.Debug("Allocate response created",
		"container_responses_count", len(finalResponse.ContainerResponses))

	return finalResponse, nil
}

// Note: GetDevicePluginOptions, GetPreferredAllocation, and PreStartContainer
// are handled by the embedded pluginapi.UnimplementedDevicePluginServer
// which provides appropriate "not implemented" responses.

// allocateContainer allocates devices for a container
func (p *VideoDevicePlugin) allocateContainer(req *pluginapi.ContainerAllocateRequest) (*pluginapi.ContainerAllocateResponse, error) {
	// Get the number of devices requested
	deviceCount := len(req.DevicesIDs)

	if deviceCount == 0 {
		return &pluginapi.ContainerAllocateResponse{}, nil
	}

	p.logger.Info("Allocating devices for container", "device_count", deviceCount, "device_ids", req.DevicesIDs)

	// Kubelet tells us which device to allocate
	deviceID := req.DevicesIDs[0] // Kubelet tells us which specific device to allocate

	// Get the device information (no allocation state tracking needed)
	device, err := p.v4l2Manager.GetDeviceByID(deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get device %s: %w", deviceID, err)
	}

	// Create environment variable
	envVars := map[string]string{
		"VIDEO_DEVICE": device.Path,
	}

	// Create device specification - mount actual device to same path in container
	devices := []*pluginapi.DeviceSpec{
		{
			ContainerPath: device.Path, // Mount to same path as host (video{VideoDeviceStartNumber}, etc.)
			HostPath:      device.Path, // Actual device on host (video{VideoDeviceStartNumber}, etc.)
			Permissions:   "rw",
		},
	}

	p.logger.Info("Allocated device",
		"device_id", device.ID,
		"host_path", device.Path,
		"container_path", device.Path,
		"env_var", fmt.Sprintf("VIDEO_DEVICE=%s", device.Path))

	response := &pluginapi.ContainerAllocateResponse{
		Devices: devices,
		Envs:    envVars,
	}

	return response, nil
}

// GetHealthStatus returns the health status of the device plugin
func (p *VideoDevicePlugin) GetHealthStatus() *HealthCheck {
	v4l2Healthy := p.v4l2Manager.IsHealthy(p.config.MaxDevices)
	devicesReady := p.v4l2Manager.GetDeviceCount(p.config.MaxDevices) > 0

	healthy := v4l2Healthy && devicesReady

	var errors []string
	if !v4l2Healthy {
		errors = append(errors, "V4L2 system is not healthy")
	}
	if !devicesReady {
		errors = append(errors, "No devices are ready")
	}

	return &HealthCheck{
		Healthy:      healthy,
		V4L2Healthy:  v4l2Healthy,
		DevicesReady: devicesReady,
		LastChecked:  time.Now(),
		Errors:       errors,
	}
}

// monitorKubeletRestart monitors for kubelet restarts and re-registers when needed
func (p *VideoDevicePlugin) monitorKubeletRestart() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			// Check if kubelet socket still exists
			if !fileExists(p.config.KubeletSocket) {
				p.logger.Warn("Kubelet socket not found, kubelet may have restarted")

				// Wait for kubelet to come back up
				for {
					select {
					case <-p.stopCh:
						return
					case <-time.After(5 * time.Second):
						if fileExists(p.config.KubeletSocket) {
							p.logger.Info("Kubelet socket found, attempting re-registration")

							// Reset registration status
							p.mu.Lock()
							p.registered = false
							p.mu.Unlock()

							// Re-register with kubelet
							if err := p.RegisterWithKubelet(); err != nil {
								p.logger.Error("Failed to re-register with kubelet", "error", err)
								continue
							}

							p.logger.Info("Successfully re-registered with kubelet after restart")
							// Continue outer monitoring loop for future restarts
							goto continueOuter
						}
					}
				}
			}
		}
	continueOuter:
		// label target for goto to resume outer loop
	}
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
