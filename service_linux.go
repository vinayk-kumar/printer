//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Linux: a systemd user service that starts at boot (via lingering) and
// restarts if it ever stops.
const unitName = "print-agent.service"

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", unitName)
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func installService(exe string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemd not found; start the agent yourself with: %s run", exe)
	}
	unit := fmt.Sprintf(`[Unit]
Description=Print Agent (prints emailed PDFs)
After=network-online.target

[Service]
ExecStart="%s" run
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, exe)
	if err := os.MkdirAll(filepath.Dir(unitPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath(), []byte(unit), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unitName}, {"restart", unitName}} {
		if err := systemctl(args...); err != nil {
			return err
		}
	}
	// Keep running after logout and start at boot without logging in.
	_ = exec.Command("loginctl", "enable-linger", os.Getenv("USER")).Run()
	return nil
}

func stopService() error { return systemctl("stop", unitName) }

func uninstallService() error {
	_ = systemctl("disable", "--now", unitName)
	if err := os.Remove(unitPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return systemctl("daemon-reload")
}

func serviceStatus() string {
	out, _ := exec.Command("systemctl", "--user", "is-active", unitName).Output()
	if s := strings.TrimSpace(string(out)); s == "active" {
		return "running"
	} else if s != "" {
		return s
	}
	return "not running"
}
