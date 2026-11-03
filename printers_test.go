package main

import (
	"reflect"
	"testing"
)

func TestParseLpstatDevices(t *testing.T) {
	out := "device for HP_LaserJet_M1005: hp:/usb/HP_LaserJet_M1005?serial=KJ1234\n" +
		"device for HP_M126nw: ipp://192.168.4.84/ipp/print\n" +
		"device for Brother: usb://Brother/HL-1110%20series?serial=E123\n" +
		"device for Office: dnssd://HP%20LaserJet._ipp._tcp.local./?uuid=1\n" +
		"device for Old: socket://10.0.0.7:9100\n"
	want := []printerInfo{
		{"HP_LaserJet_M1005", "USB"},
		{"HP_M126nw", "network 192.168.4.84"},
		{"Brother", "USB"},
		{"Office", "network"},
		{"Old", "network 10.0.0.7"},
	}
	if got := parseLpstatDevices(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestParseWindowsPrinters(t *testing.T) {
	out := "HP LaserJet M1005|USB001\r\n" +
		"Microsoft Print to PDF|PORTPROMPT:\r\n" +
		"OneNote (Desktop)|nul:\r\n" +
		"Fax|SHRFAX:\r\n" +
		"HP M126nw|IP_192.168.4.84\r\n" +
		"PrintAgent|http://192.168.4.84:631/ipp/print\r\n" +
		"Office WSD|WSD-1234-5678\r\n"
	want := []printerInfo{
		{"HP LaserJet M1005", "USB"},
		{"HP M126nw", "network 192.168.4.84"},
		{"PrintAgent", "network 192.168.4.84"},
		{"Office WSD", "network"},
	}
	if got := parseWindowsPrinters(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestParsePrinterChoice(t *testing.T) {
	printers := []printerInfo{{"HP LaserJet M1005", "USB"}, {"HP_M126nw", "network"}}
	for _, tc := range []struct {
		answer, ip, name string
		ok               bool
	}{
		{"1", "", "HP LaserJet M1005", true},
		{"2", "", "HP_M126nw", true},
		{"hp_m126nw", "", "HP_M126nw", true},
		{"192.168.1.50", "192.168.1.50", "", true},
		{"printer.local", "printer.local", "", true},
		{"3", "", "", false},
		{"kitchen", "", "", false},
		{"", "", "", true},
	} {
		ip, name, err := parsePrinterChoice(tc.answer, printers)
		if (err == nil) != tc.ok || ip != tc.ip || name != tc.name {
			t.Errorf("parsePrinterChoice(%q) = %q, %q, %v", tc.answer, ip, name, err)
		}
	}
}
