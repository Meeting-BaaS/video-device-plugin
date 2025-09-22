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
	updateCh    chan struct{} // Channel to trigger ListAndWatch updates
}

// NewVideoDevicePlugin creates a new VideoDevicePlugin instance
func NewVideoDevicePlugin(config *DevicePluginConfig, v4l2Manager V4L2Manager, logger *slog.Logger) *VideoDevicePlugin {
	plugin := &VideoDevicePlugin{
		config:      config,
		v4l2Manager: v4l2Manager,
		logger:      logger,
		stopCh:      make(chan struct{}),
		registered:  false,
		updateCh:    make(chan struct{}, 1), // Buffered channel for immediate updates
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
	go func() {
		p.logger.Info("Starting gRPC server", "socket", p.config.SocketPath)
		if err := p.server.Serve(listener); err != nil {
			p.logger.Error("gRPC server failed", "error", err)
		}
	}()

	// Wait for server to start
	time.Sleep(1 * time.Second)

	// Register with kubelet
	if err := p.RegisterWithKubelet(); err != nil {
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
	conn, err := grpc.Dial("unix://"+p.config.KubeletSocket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to kubelet: %w", err)
	}
	defer conn.Close()

	// Create registration client
	client := pluginapi.NewRegistrationClient(conn)

	// Create registration request
	req := &pluginapi.RegisterRequest{
		Version:      "v1beta1",
		Endpoint:     filepath.Base(p.config.SocketPath),
		ResourceName: p.config.ResourceName,
	}

	// Send registration request
	_, err = client.Register(context.Background(), req)
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

	// Send initial device list
	if err := p.sendDeviceList(stream); err != nil {
		return err
	}

	// Watch for changes
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			p.logger.Debug("ListAndWatch stopping")
			return nil
		case <-p.updateCh:
			// Immediate update triggered by device allocation
			if err := p.sendDeviceList(stream); err != nil {
				p.logger.Error("Failed to send device list update", "error", err)
				return err
			}
		case <-ticker.C:
			// Periodic health check
			if err := p.sendDeviceList(stream); err != nil {
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

// GetDevicePluginOptions implements the GetDevicePluginOptions gRPC method
func (p *VideoDevicePlugin) GetDevicePluginOptions(ctx context.Context, req *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	p.logger.Debug("GetDevicePluginOptions called")

	return &pluginapi.DevicePluginOptions{
		PreStartRequired: false,
		GetPreferredAllocationAvailable: false,
	}, nil
}

// GetPreferredAllocation implements the GetPreferredAllocation gRPC method
func (p *VideoDevicePlugin) GetPreferredAllocation(ctx context.Context, req *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	p.logger.Debug("GetPreferredAllocation called")
	
	// For now, just return the requested devices as preferred
	return &pluginapi.PreferredAllocationResponse{
		ContainerResponses: []*pluginapi.ContainerPreferredAllocationResponse{
			{
				DeviceIDs: req.ContainerRequests[0].AvailableDeviceIDs,
			},
		},
	}, nil
}

// PreStartContainer implements the PreStartContainer gRPC method
func (p *VideoDevicePlugin) PreStartContainer(ctx context.Context, req *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	p.logger.Debug("PreStartContainer called")
	
	// We don't need to do anything before container start
	return &pluginapi.PreStartContainerResponse{}, nil
}

// sendDeviceList sends the current device list to the client
func (p *VideoDevicePlugin) sendDeviceList(stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	allDevices := p.v4l2Manager.ListAllDevices()
	
	var devices []*pluginapi.Device
	for _, device := range allDevices {
		// Only send unallocated devices to kubelet
		if !device.Allocated {
			devices = append(devices, &pluginapi.Device{
				ID:     device.ID,
				Health: pluginapi.Healthy,
			})
		}
	}

	response := &pluginapi.ListAndWatchResponse{
		Devices: devices,
	}

	p.logger.Debug("Sending device list", "device_count", len(devices))
	return stream.Send(response)
}

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
	
	// Allocate the specific device requested by kubelet
	// Kubernetes ensures the device is available before requesting it
	device, err := p.v4l2Manager.AllocateDevice(deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate device %s: %w", deviceID, err)
	}

	// Trigger immediate ListAndWatch update to notify kubelet that device is no longer available
	select {
	case p.updateCh <- struct{}{}:
		p.logger.Debug("Triggered immediate ListAndWatch update")
	default:
		p.logger.Debug("ListAndWatch update channel full, will update on next tick")
	}

	// Create environment variable
	envVars := map[string]string{
		"VIDEO_DEVICE": device.Path,
	}

	// Create device specification - mount actual device to same path in container
	devices := []*pluginapi.DeviceSpec{
		{
			ContainerPath: device.Path,  // Mount to same path as host (video{VideoDeviceStartNumber}, etc.)
			HostPath:      device.Path,  // Actual device on host (video{VideoDeviceStartNumber}, etc.)
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
		// No annotations needed - Kubernetes handles device allocation
	}

	return response, nil
}

// GetHealthStatus returns the health status of the device plugin
func (p *VideoDevicePlugin) GetHealthStatus() *HealthCheck {
	v4l2Healthy := p.v4l2Manager.IsHealthy()
	devicesReady := p.v4l2Manager.GetDeviceCount() > 0

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
							break
						}
					}
				}
			}
		}
	}
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

