package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// loadV4L2LoopbackModule loads the v4l2loopback kernel module
func loadV4L2LoopbackModule(logger *slog.Logger) error {
	logger.Info("📦 Loading v4l2loopback kernel module...")
	
	// Check if module is already loaded
	if lsmodOutput, err := exec.Command("lsmod").Output(); err == nil {
		if strings.Contains(string(lsmodOutput), "v4l2loopback") {
			logger.Info("✅ v4l2loopback module already loaded")
			return nil
		}
	}
	
	// CRITICAL: Load videodev module first (required for v4l2loopback)
	logger.Info("🔧 Loading videodev module (required for v4l2loopback)...")
	if err := exec.Command("modprobe", "videodev").Run(); err != nil {
		logger.Error("❌ Failed to load videodev module - this is required for v4l2loopback")
		logger.Info("Make sure linux-modules-extra-$(uname -r) is installed")
		return fmt.Errorf("failed to load videodev module: %w", err)
	}
	
	// Verify videodev is loaded
	if lsmodOutput, err := exec.Command("lsmod").Output(); err == nil {
		if strings.Contains(string(lsmodOutput), "videodev") {
			logger.Info("✅ videodev module loaded successfully")
		} else {
			logger.Error("❌ ERROR: videodev module is not loaded - v4l2loopback will fail")
			return fmt.Errorf("videodev module not loaded")
		}
	}
	
	// Load the v4l2loopback module with our specific parameters
	cmd := exec.Command("modprobe", "v4l2loopback",
		"devices=8",
		"max_buffers=2",
		"exclusive_caps=1",
		`card_label="MeetingBot_WebCam"`)
	
	if err := cmd.Run(); err != nil {
		logger.Error("❌ Failed to load v4l2loopback module")
		logger.Info("Checking dmesg for errors:")
		if dmesgOutput, dmesgErr := exec.Command("dmesg").Output(); dmesgErr == nil {
			lines := strings.Split(string(dmesgOutput), "\n")
			for i := len(lines) - 10; i < len(lines); i++ {
				if i >= 0 {
					logger.Info("   " + lines[i])
				}
			}
		}
		return fmt.Errorf("failed to load v4l2loopback module: %w", err)
	}
	
	logger.Info("✅ v4l2loopback module loaded successfully")
	return nil
}

// cleanupV4L2Module unloads the v4l2loopback module on shutdown
func cleanupV4L2Module(logger *slog.Logger) {
	logger.Info("🧹 Cleaning up v4l2loopback module")
	
	// Check if v4l2loopback module is loaded
	cmd := exec.Command("lsmod")
	output, err := cmd.Output()
	if err != nil {
		logger.Warn("Failed to check loaded modules", "error", err)
		return
	}
	
	if !strings.Contains(string(output), "v4l2loopback") {
		logger.Info("ℹ️  v4l2loopback module not loaded, nothing to cleanup")
		return
	}
	
	// Unload v4l2loopback module
	logger.Info("📦 Unloading v4l2loopback module...")
	unloadCmd := exec.Command("modprobe", "-r", "v4l2loopback")
	if err := unloadCmd.Run(); err != nil {
		logger.Warn("⚠️  Failed to unload v4l2loopback module", "error", err)
		logger.Info("Module may be in use by other processes")
	} else {
		logger.Info("✅ v4l2loopback module unloaded successfully")
	}
	
	// Check if videodev module can be unloaded (if not needed by other modules)
	if strings.Contains(string(output), "videodev") {
		logger.Info("Checking if videodev module can be unloaded")
		// Check if any other video modules are using videodev
		lsmodCmd := exec.Command("lsmod")
		lsmodOutput, err := lsmodCmd.Output()
		if err == nil && !strings.Contains(string(lsmodOutput), "v4l2loopback") {
			// No other modules using videodev, try to unload it
			unloadVideodevCmd := exec.Command("modprobe", "-r", "videodev")
			if err := unloadVideodevCmd.Run(); err != nil {
				logger.Info("videodev module still needed by other modules, keeping loaded")
			} else {
				logger.Info("✅ videodev module unloaded successfully")
			}
		}
	}
	
	logger.Info("✅ Cleanup completed")
}
