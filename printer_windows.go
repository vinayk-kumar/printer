//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// On Windows the agent prints through a Windows printer, using SumatraPDF
// (downloaded by install.ps1 next to print-agent.exe) to print PDFs silently.
const queueName = "PrintAgent"

func powershell(script string) (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func listPrinters() ([]printerInfo, error) {
	out, err := powershell(`Get-Printer | ForEach-Object { $_.Name + "|" + $_.PortName }`)
	if err != nil {
		return nil, fmt.Errorf("could not list printers: %s", out)
	}
	return parseWindowsPrinters(out), nil
}

// setupPrinter reuses a Windows printer already installed for this IP, or
// adds one over IPP (Windows 11). Returns the Windows printer name.
func setupPrinter(ip string) (string, error) {
	script := fmt.Sprintf(`
$ip = '%s'; $name = '%s'
$ports = Get-PrinterPort | Where-Object { $_.PrinterHostAddress -eq $ip -or $_.Name -like "*$ip*" } | ForEach-Object Name
$existing = Get-Printer | Where-Object { $ports -contains $_.PortName -or $_.PortName -like "*$ip*" } | Select-Object -First 1
if ($existing) { $existing.Name; exit 0 }
Add-Printer -Name $name -IppURL "http://${ip}:631/ipp/print" -ErrorAction Stop
$name`, ip, queueName)
	out, err := powershell(script)
	if err != nil || out == "" {
		return "", fmt.Errorf("could not add the printer automatically (%s).\n"+
			"  Add it once in Windows: Settings > Bluetooth & devices > Printers & scanners > Add device\n"+
			"  (or 'Add manually' with IP %s), then run the install command again and choose it from the list", out, ip)
	}
	lines := strings.Split(out, "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

// printFile prints through SumatraPDF and returns the file's name, which is
// the document name of the Windows print job.
func printFile(c *Config, path, title string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	sumatra := filepath.Join(filepath.Dir(exe), "SumatraPDF.exe")
	if _, err := os.Stat(sumatra); err != nil {
		return "", fmt.Errorf("SumatraPDF.exe is missing next to print-agent.exe; run the installer again")
	}
	out, err := exec.Command(sumatra, "-print-to", c.PrinterName, "-print-settings", "fit", "-silent", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("SumatraPDF: %v %s", err, strings.TrimSpace(string(out)))
	}
	return filepath.Base(path), nil
}

// psQuote quotes a value for a single-quoted PowerShell string.
func psQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

// jobState tells whether a print job is still in the Windows print queue,
// and what is wrong with the printer if something is.
func jobState(c *Config, job string) (busy bool, problem string) {
	if job == "" {
		return false, ""
	}
	out, err := powershell(fmt.Sprintf(`Get-PrintJob -PrinterName '%s' | Where-Object { $_.DocumentName -like '*%s*' } | ForEach-Object { [string]$_.JobStatus }`,
		psQuote(c.PrinterName), psQuote(job)))
	if err != nil || out == "" {
		return false, ""
	}
	_, problem = printerHealth(c)
	if problem == "" && (strings.Contains(out, "Error") || strings.Contains(out, "Blocked") || strings.Contains(out, "UserIntervention")) {
		problem = "Printer error (" + out + ")"
	}
	return true, problem
}

// printerHealth returns "ready", "printing", "problem" or "missing", and the
// problem in plain words.
func printerHealth(c *Config) (state, problem string) {
	out, err := powershell(fmt.Sprintf(`[string](Get-Printer -Name '%s' -ErrorAction Stop).PrinterStatus`, psQuote(c.PrinterName)))
	if err != nil || out == "" {
		return "missing", "Not found on this computer"
	}
	return windowsPrinterHealth(out)
}

func recoverPrinter(c *Config) {}

func printerStatus(c *Config) string {
	out, err := powershell(fmt.Sprintf(`(Get-Printer -Name '%s').PrinterStatus`, c.PrinterName))
	if err != nil || out == "" {
		return "not found"
	}
	return out
}

func removePrinter(c *Config) {
	if c.PrinterName == queueName {
		_, _ = powershell(fmt.Sprintf(`Remove-Printer -Name '%s'`, queueName))
	}
}
