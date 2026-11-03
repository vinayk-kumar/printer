//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Windows: a hidden launcher in the user's Startup folder starts the agent
// at login (no admin rights needed).
func launcherPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "PrintAgent.vbs")
}

func installService(exe string) error {
	vbs := fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"\"\"%s\"\" run\", 0, False\r\n", exe)
	if err := os.WriteFile(launcherPath(), []byte(vbs), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("wscript.exe", launcherPath()).CombinedOutput(); err != nil {
		return fmt.Errorf("could not start the agent: %v %s", err, out)
	}
	return nil
}

func stopService() error {
	_, err := powershell(fmt.Sprintf(`Get-Process print-agent -ErrorAction SilentlyContinue | Where-Object Id -ne %d | Stop-Process -Force`, os.Getpid()))
	return err
}

func uninstallService() error {
	if err := os.Remove(launcherPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func serviceStatus() string {
	out, _ := powershell(fmt.Sprintf(`@(Get-Process print-agent -ErrorAction SilentlyContinue | Where-Object Id -ne %d).Count`, os.Getpid()))
	if strings.TrimSpace(out) != "0" && strings.TrimSpace(out) != "" {
		return "running"
	}
	return "not running"
}
