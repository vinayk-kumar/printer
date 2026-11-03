//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// macOS: a launchd LaunchAgent that starts at login and restarts if it stops.
const launchLabel = "com.printagent.agent"

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
}

func installService(exe string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>run</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
`, launchLabel, exe)
	if err := os.MkdirAll(filepath.Dir(plistPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath(), []byte(plist), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", plistPath()).Run()
	if out, err := exec.Command("launchctl", "load", "-w", plistPath()).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %s", out)
	}
	return nil
}

func stopService() error { return exec.Command("launchctl", "unload", plistPath()).Run() }

func uninstallService() error {
	_ = stopService()
	if err := os.Remove(plistPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func serviceStatus() string {
	if exec.Command("launchctl", "list", launchLabel).Run() == nil {
		return "running"
	}
	return "not running"
}
