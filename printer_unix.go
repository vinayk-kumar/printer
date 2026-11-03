//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// On Linux and macOS printing goes through CUPS. A network printer given by
// IP is added as an IPP Everywhere / AirPrint queue; USB printers (and any
// other printer) are used as installed in the system.
const queueName = "PrintAgent"

// cups runs a CUPS command with English output, so it can be parsed.
func cups(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd
}

func listPrinters() ([]printerInfo, error) {
	if _, err := exec.LookPath("lpstat"); err != nil {
		return nil, fmt.Errorf("the CUPS printing system is not installed (Ubuntu/Debian/Raspberry Pi: sudo apt install cups)")
	}
	out, _ := cups("lpstat", "-v").Output() // fails when no printer is installed yet
	return parseLpstatDevices(string(out)), nil
}

func setupPrinter(ip string) (string, error) {
	if _, err := exec.LookPath("lpadmin"); err != nil {
		return "", fmt.Errorf("the CUPS printing system is not installed (Ubuntu/Debian/Raspberry Pi: sudo apt install cups)")
	}
	uri := fmt.Sprintf("ipp://%s/ipp/print", ip)
	if out, err := cups("lpstat", "-v", queueName).CombinedOutput(); err == nil && strings.Contains(string(out), uri) {
		_ = cups("cupsenable", queueName).Run()
		return queueName, nil
	}
	args := []string{"-p", queueName, "-E", "-v", uri, "-m", "everywhere", "-D", "Print Agent (" + ip + ")"}
	out, err := cups("lpadmin", args...).CombinedOutput()
	if err == nil {
		return queueName, nil
	}
	if strings.Contains(strings.ToLower(string(out)), "forbidden") && runtime.GOOS == "linux" {
		// The user isn't in the lpadmin group yet: ask for the sudo password.
		fmt.Println("\n  Adding a printer needs administrator rights:")
		sudo := exec.Command("sudo", append([]string{"lpadmin"}, args...)...)
		sudo.Stdin, sudo.Stdout, sudo.Stderr = os.Stdin, os.Stdout, os.Stderr
		if sudo.Run() == nil {
			return queueName, nil
		}
	}
	return "", fmt.Errorf("could not add printer: %s (the printer must support IPP Everywhere / AirPrint; "+
		"otherwise install it in the system's printer settings and choose it from the list)", strings.TrimSpace(string(out)))
}

func printFile(c *Config, path, title string) error {
	out, err := cups("lp", "-d", c.PrinterName, "-t", title, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// recoverPrinter resumes the queue when CUPS paused it after a printer error
// (e.g. out of paper), so jobs don't stay stuck after the problem is fixed.
func recoverPrinter(c *Config) {
	if out, err := cups("lpstat", "-p", c.PrinterName).Output(); err == nil && strings.Contains(string(out), "disabled") {
		_ = cups("cupsenable", c.PrinterName).Run()
	}
}

func printerStatus(c *Config) string {
	out, err := cups("lpstat", "-p", c.PrinterName).CombinedOutput()
	if err != nil {
		return "not found"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	status := strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		status += " / " + strings.TrimSpace(lines[1])
	}
	return status
}

func removePrinter(c *Config) {
	if c.PrinterName == queueName {
		_ = cups("lpadmin", "-x", queueName).Run()
	}
}
