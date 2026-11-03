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

func findPrinter(printers []printerInfo, name string) (printerInfo, bool) {
	for _, p := range printers {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return printerInfo{}, false
}
