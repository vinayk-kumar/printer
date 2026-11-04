package main

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// printerInfo is a printer installed on this computer (USB or network).
type printerInfo struct {
	Name       string
	Connection string // "USB", "network 192.168.1.50", ...
}

// parseLpstatDevices reads `lpstat -v` output: "device for NAME: URI".
func parseLpstatDevices(out string) []printerInfo {
	var list []printerInfo
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "device for ")
		if !ok {
			continue
		}
		if name, uri, ok := strings.Cut(rest, ": "); ok {
			list = append(list, printerInfo{Name: name, Connection: describeURI(uri)})
		}
	}
	return list
}

func describeURI(uri string) string {
	switch {
	case strings.HasPrefix(uri, "usb:"), strings.HasPrefix(uri, "hp:/usb"):
		return "USB"
	case strings.HasPrefix(uri, "dnssd:"), strings.HasPrefix(uri, "hp:/net"):
		return "network"
	}
	if u, err := url.Parse(uri); err == nil && u.Hostname() != "" {
		return "network " + u.Hostname()
	}
	return uri
}

// parseWindowsPrinters reads "NAME|PORT" lines from Get-Printer, skipping
// virtual printers (PDF, XPS, OneNote, Fax).
func parseWindowsPrinters(out string) []printerInfo {
	var list []printerInfo
	for _, line := range strings.Split(out, "\n") {
		name, port, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || name == "" {
			continue
		}
		lower := strings.ToLower(name + " " + port)
		if strings.Contains(lower, "print to pdf") || strings.Contains(lower, "xps") ||
			strings.Contains(lower, "onenote") || strings.Contains(lower, "fax") ||
			strings.EqualFold(port, "PORTPROMPT:") || strings.EqualFold(port, "nul:") {
			continue
		}
		conn := port
		switch upper := strings.ToUpper(port); {
		case strings.HasPrefix(upper, "USB"):
			conn = "USB"
		case strings.HasPrefix(upper, "WSD"):
			conn = "network"
		case strings.HasPrefix(upper, "HTTP"):
			conn = describeURI(port)
		default:
			if ip := strings.TrimPrefix(port, "IP_"); net.ParseIP(ip) != nil {
				conn = "network " + ip
			}
		}
		list = append(list, printerInfo{Name: name, Connection: conn})
	}
	return list
}

// parsePrinterChoice turns the answer to "choose a number, or type an IP"
// into either a printer IP (network printer to add) or an installed printer.
func parsePrinterChoice(answer string, printers []printerInfo) (ip, name string, err error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", "", nil
	}
	if n, err := strconv.Atoi(answer); err == nil {
		if n >= 1 && n <= len(printers) {
			return "", printers[n-1].Name, nil
		}
		return "", "", fmt.Errorf("there is no printer number %d", n)
	}
	for _, p := range printers {
		if strings.EqualFold(p.Name, answer) {
			return "", p.Name, nil
		}
	}
	if net.ParseIP(answer) != nil || strings.Contains(answer, ".") {
		return answer, "", nil
	}
	return "", "", fmt.Errorf("%q is not a printer number, an installed printer or an IP address", answer)
}

// Printer alerts (IPP printer-state-reasons, the same for every brand) in
// plain words. Warnings such as "toner low" are not problems.
var printerAlerts = []struct{ reason, text string }{
	{"media-empty", "Out of paper"},
	{"media-needed", "Out of paper"},
	{"media-jam", "Paper jam"},
	{"toner-empty", "Out of toner"},
	{"marker-supply-empty", "Out of toner or ink"},
	{"door-open", "A cover is open"},
	{"cover-open", "A cover is open"},
	{"input-tray-missing", "Paper tray missing"},
	{"offline", "Printer is offline"},
	{"paused", "Printer is paused"},
}

// windowsPrinterHealth maps Get-Printer's PrinterStatus (e.g. "Normal",
// "PaperOut", "Offline, Error") to a state and a problem in plain words.
func windowsPrinterHealth(status string) (state, problem string) {
	words := map[string]string{
		"PaperOut": "Out of paper", "PaperProblem": "Paper problem", "PaperJam": "Paper jam",
		"NoToner": "Out of toner", "DoorOpen": "A cover is open", "Offline": "Printer is offline",
		"Paused": "Printer is paused", "NotAvailable": "Printer is not available",
		"UserIntervention": "The printer needs attention", "Error": "Printer error",
	}
	state = "ready"
	for _, s := range strings.Split(status, ",") {
		s = strings.TrimSpace(s)
		if text, ok := words[s]; ok && problem == "" {
			problem = text
		}
		if s == "Printing" || s == "Busy" || s == "Processing" || s == "WarmingUp" {
			state = "printing"
		}
	}
	if problem != "" {
		state = "problem"
	}
	return state, problem
}

// Printer messages from CUPS that mean the printer can't print right now.
var problemMessages = []string{"not responding", "unable", "become available", "not connected", "offline", "error", "failed"}

// parseLpstatPrinter reads `lpstat -l -p NAME`:
//
//	printer NAME is idle.  enabled since ...      (or "now printing", "disabled")
//		Waiting for printer to become available.  (optional message)
//		...
//		Alerts: media-empty-error, offline-report
func parseLpstatPrinter(out string) (state, problem string) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "printer ") {
		return "missing", "Not found on this computer"
	}
	first := lines[0]
	state = "ready"
	if strings.Contains(first, "now printing") {
		state = "printing"
	}
	message := ""
	if len(lines) > 1 {
		if m := strings.TrimSpace(lines[1]); !strings.HasSuffix(m, ":") { // not "Form mounted:"
			message = m
		}
	}
	for _, line := range lines[1:] {
		reasons, ok := strings.CutPrefix(strings.TrimSpace(line), "Alerts:")
		if !ok {
			continue
		}
		for _, reason := range strings.Split(reasons, ",") {
			reason = strings.TrimSpace(reason)
			if strings.HasSuffix(reason, "-warning") {
				continue
			}
			for _, a := range printerAlerts {
				if strings.HasPrefix(reason, a.reason) && problem == "" {
					problem = a.text
				}
			}
		}
	}
	lower := strings.ToLower(message)
	for _, p := range problemMessages {
		if problem == "" && strings.Contains(lower, p) {
			problem = strings.TrimSuffix(message, ".")
		}
	}
	if strings.Contains(first, " disabled ") && problem == "" {
		problem = firstOf(strings.TrimSuffix(message, "."), "Stopped by the printing system")
	}
	if problem != "" {
		state = "problem"
	}
	return state, problem
}

func findPrinter(printers []printerInfo, name string) (printerInfo, bool) {
	for _, p := range printers {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return printerInfo{}, false
}
