# video-device-plugin
A Kubernetes device plugin that manages v4l2loopback virtual camera devices for meeting bots. The plugin runs as a DaemonSet, pre-installs v4l2loopback kernel module, creates video devices, and provides device allocation to pods.
