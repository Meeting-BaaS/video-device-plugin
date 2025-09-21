package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// checkRoot checks if the application is running as root
func checkRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this container must run as root to access kernel modules")
	}
	fmt.Println("✅ Running as root")
	return nil
}

// displaySystemInfo displays system information
func displaySystemInfo(logger *slog.Logger) {
	logger.Info("📊 System Information:")
	
	// Get kernel version
	if kernelInfo, err := exec.Command("uname", "-r").Output(); err == nil {
		logger.Info("   Kernel version: " + strings.TrimSpace(string(kernelInfo)))
	}
	
	// Get architecture
	if archInfo, err := exec.Command("uname", "-m").Output(); err == nil {
		logger.Info("   Architecture: " + strings.TrimSpace(string(archInfo)))
	}
	
	// Get memory info
	if memInfo, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(memInfo), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				logger.Info("   Available memory: " + strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:")))
				break
			}
		}
	}
	
	// Count loaded v4l2 modules
	if lsmodOutput, err := exec.Command("lsmod").Output(); err == nil {
		v4l2Count := strings.Count(string(lsmodOutput), "v4l2")
		logger.Info("   Loaded modules: " + fmt.Sprintf("%d v4l2 modules", v4l2Count))
	}
}
