package main

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Is the printer switched on and connected? The print system only notices
// when a job fails, so the agent checks it directly: a network printer must
// accept a connection, a USB printer must be plugged in (Linux).

const printerOffline = "Printer is off or not connected"

var connCache struct {
	sync.Mutex
	key     string
	at      time.Time
	problem string
}

// connectionProblem returns printerOffline when the printer can't be
// reached, "" when it can or when that can't be checked. Results are kept
// for 10 seconds, since the status page and the dashboard both ask.
func connectionProblem(c *Config) string {
	key := c.PrinterName + "|" + c.PrinterIP
	connCache.Lock()
	if connCache.key == key && time.Since(connCache.at) < 10*time.Second {
		defer connCache.Unlock()
		return connCache.problem
	}
	connCache.Unlock()

	problem := ""
	if ip := c.PrinterIP; ip != "" {
		if !dialAny([]string{net.JoinHostPort(ip, "631"), net.JoinHostPort(ip, "9100"), net.JoinHostPort(ip, "80")}) {
			problem = printerOffline
		}
	} else if addr, serial := probeTarget(printerDeviceURI(c)); addr != "" {
		if !dialAny([]string{addr}) {
			problem = printerOffline
		}
	} else if serial != "" && !usbSerialPresent(serial) {
		problem = "Printer is off or its USB cable is unplugged"
	}

	connCache.Lock()
	connCache.key, connCache.at, connCache.problem = key, time.Now(), problem
	connCache.Unlock()
	return problem
}

func printerReachable(ip string) error {
	var lastErr error
	for _, port := range []string{"631", "9100", "80"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), 3*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// dialAny tries the addresses at once and tells whether any accepted a connection.
func dialAny(addrs []string) bool {
	ok := make(chan bool, len(addrs))
	for _, a := range addrs {
		go func() {
			conn, err := net.DialTimeout("tcp", a, 3*time.Second)
			if err == nil {
				conn.Close()
			}
			ok <- err == nil
		}()
	}
	for range addrs {
		if <-ok {
			return true
		}
	}
	return false
}

// probeTarget reads a printer's device URI: the host:port to connect to for
// a network printer (or an IPP-over-USB printer on localhost), or the serial
// number of a USB printer. Both are empty when it can't be checked
// (e.g. dnssd:// names).
func probeTarget(uri string) (addr, usbSerial string) {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return "", ""
	}
	q := u.Query()
	switch u.Scheme {
	case "usb":
		return "", q.Get("serial")
	case "hp", "hpfax":
		if strings.HasPrefix(u.Path, "/usb/") {
			return "", q.Get("serial")
		}
		if ip := q.Get("ip"); ip != "" { // hp:/net/NAME?ip=192.168.1.5
			return net.JoinHostPort(ip, "9100"), ""
		}
		return "", ""
	}
	ports := map[string]string{"ipp": "631", "ipps": "631", "http": "80", "https": "443", "socket": "9100", "lpd": "515"}
	port, ok := ports[u.Scheme]
	if !ok || u.Hostname() == "" {
		return "", ""
	}
	if u.Port() != "" {
		port = u.Port()
	}
	return net.JoinHostPort(u.Hostname(), port), ""
}

// usbSerialPresent tells whether a USB device with this serial number is
// plugged in. Only Linux can tell; elsewhere it answers yes.
var usbDevicesDir = "/sys/bus/usb/devices"

func usbSerialPresent(serial string) bool {
	files, err := filepath.Glob(filepath.Join(usbDevicesDir, "*", "serial"))
	if err != nil || len(files) == 0 {
		return true
	}
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil && strings.EqualFold(strings.TrimSpace(string(data)), serial) {
			return true
		}
	}
	return false
}
