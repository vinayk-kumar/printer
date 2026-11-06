package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeTarget(t *testing.T) {
	for _, tc := range []struct{ uri, addr, serial string }{
		{"ipp://192.168.4.84/ipp/print", "192.168.4.84:631", ""},
		{"ipps://printer.local:443/ipp/print", "printer.local:443", ""},
		{"socket://10.0.0.9", "10.0.0.9:9100", ""},
		{"ipp://localhost:60000/ipp/print", "localhost:60000", ""}, // IPP over USB
		{"usb://HP/LaserJet%20M1005?serial=KJ0ABC1", "", "KJ0ABC1"},
		{"hp:/usb/HP_LaserJet_M1005?serial=KJ0ABC1", "", "KJ0ABC1"},
		{"hp:/net/HP_LaserJet?ip=192.168.1.5", "192.168.1.5:9100", ""},
		{"dnssd://HP%20M126nw._ipp._tcp.local/", "", ""},
		{"", "", ""},
	} {
		addr, serial := probeTarget(tc.uri)
		if addr != tc.addr || serial != tc.serial {
			t.Errorf("probeTarget(%q) = %q, %q; want %q, %q", tc.uri, addr, serial, tc.addr, tc.serial)
		}
	}
}

func TestDialAny(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedAddr := closed.Addr().String()
	closed.Close()
	if !dialAny([]string{closedAddr, l.Addr().String()}) {
		t.Error("open port not found")
	}
	if dialAny([]string{closedAddr}) {
		t.Error("closed port reported open")
	}
}

func TestUSBSerialPresent(t *testing.T) {
	dir := t.TempDir()
	usbDevicesDir = dir
	t.Cleanup(func() { usbDevicesDir = "/sys/bus/usb/devices" })
	os.MkdirAll(filepath.Join(dir, "1-2"), 0o755)
	os.WriteFile(filepath.Join(dir, "1-2", "serial"), []byte("KJ0ABC1\n"), 0o644)
	if !usbSerialPresent("kj0abc1") || usbSerialPresent("OTHER") {
		t.Error("USB serial lookup is wrong")
	}
}
