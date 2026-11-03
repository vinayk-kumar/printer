//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// On Linux and macOS printing goes through CUPS, which converts PDFs and
// images to whatever the printer understands (IPP Everywhere / AirPrint).
const queueName = "PrintAgent"

func setupPrinter(ip string) (string, error) {
	if _, err := exec.LookPath("lp"); err != nil {
		return "", fmt.Errorf("the CUPS printing system is not installed (Ubuntu/Debian: sudo apt install cups)")
	}
	uri := fmt.Sprintf("ipp://%s/ipp/print", ip)
	if out, err := exec.Command("lpstat", "-v", queueName).CombinedOutput(); err == nil && strings.Contains(string(out), uri) {
		_ = exec.Command("cupsenable", queueName).Run()
		return queueName, nil
	}
	out, err := exec.Command("lpadmin", "-p", queueName, "-E", "-v", uri, "-m", "everywhere",
		"-D", "Print Agent ("+ip+")").CombinedOutput()
	if err != nil {
		hint := "the printer must support IPP Everywhere / AirPrint"
		if strings.Contains(strings.ToLower(string(out)), "forbidden") {
			hint = "your user may not add printers"
			if runtime.GOOS == "linux" {
				hint += ": sudo usermod -aG lpadmin $USER, then log out and in"
			}
		}
		return "", fmt.Errorf("could not add printer: %s (%s)", strings.TrimSpace(string(out)), hint)
	}
	return queueName, nil
}

func printFile(c *Config, path, title string) error {
	out, err := exec.Command("lp", "-d", c.PrinterName, "-t", title, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// recoverPrinter resumes the queue when CUPS paused it after a printer error
// (e.g. out of paper), so jobs don't stay stuck after the problem is fixed.
func recoverPrinter(c *Config) {
	if out, err := exec.Command("lpstat", "-p", c.PrinterName).Output(); err == nil && strings.Contains(string(out), "disabled") {
		_ = exec.Command("cupsenable", c.PrinterName).Run()
	}
}

func printerStatus(c *Config) string {
	out, err := exec.Command("lpstat", "-p", c.PrinterName).CombinedOutput()
	if err != nil {
		return "not set up"
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
		_ = exec.Command("lpadmin", "-x", queueName).Run()
	}
}
