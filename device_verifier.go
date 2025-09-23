package main

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// verifyVideoDevices verifies that video devices were created
func verifyVideoDevices(config *DevicePluginConfig, logger *slog.Logger) error {
	logger.Info("Verifying video devices...")

	deviceCount := 0
	for i := VideoDeviceStartNumber; i < VideoDeviceStartNumber+config.MaxDevices; i++ {
		devicePath := fmt.Sprintf("/dev/video%d", i)
		if stat, err := os.Stat(devicePath); err == nil {
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				logger.Warn("non-char device at expected path", "path", devicePath, "mode", stat.Mode().String())
				continue
			}
			deviceCount++
			if st, ok := stat.Sys().(*syscall.Stat_t); ok {
				maj := unix.Major(uint64(st.Rdev))
				min := unix.Minor(uint64(st.Rdev))
				logger.Info("video device",
					"path", devicePath,
					"mode", stat.Mode().String(),
					"uid", st.Uid,
					"gid", st.Gid,
					"rdev", fmt.Sprintf("%d,%d", maj, min),
					"mtime", stat.ModTime())
			} else {
				logger.Info("video device", "path", devicePath, "mode", stat.Mode().String())
			}
		}
	}

	if deviceCount == 0 {
		return fmt.Errorf("no video devices found")
	}

	logger.Info("video devices found", "count", deviceCount, "requested", config.MaxDevices)
	return nil
}
