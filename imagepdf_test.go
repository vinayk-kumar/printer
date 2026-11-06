package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testJPEG makes a w×h JPEG with the given EXIF orientation (1 = none).
func testJPEG(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	if orientation == 1 {
		return b.Bytes()
	}
	// APP1 "Exif" segment, big-endian TIFF with one IFD0 entry: Orientation.
	var tiff bytes.Buffer
	tiff.WriteString("MM\x00\x2a")
	binary.Write(&tiff, binary.BigEndian, uint32(8))
	binary.Write(&tiff, binary.BigEndian, uint16(1))
	binary.Write(&tiff, binary.BigEndian, []uint16{0x0112, 3})
	binary.Write(&tiff, binary.BigEndian, uint32(1))
	binary.Write(&tiff, binary.BigEndian, []uint16{uint16(orientation), 0})
	binary.Write(&tiff, binary.BigEndian, uint32(0))
	seg := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, b.Bytes()[2:]...)
}

func TestJPEGOrientation(t *testing.T) {
	for _, o := range []int{1, 3, 6, 8} {
		if got := jpegOrientation(testJPEG(t, 8, 4, o)); got != o {
			t.Errorf("orientation %d read as %d", o, got)
		}
	}
	if got := jpegOrientation([]byte("not a jpeg")); got != 1 {
		t.Errorf("garbage: %d", got)
	}
}

// pdfPageSize checks the PDF with pdfinfo and returns its page size, e.g. "595 x 842 pts (A4)".
func pdfPageSize(t *testing.T, pdf []byte) string {
	t.Helper()
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed")
	}
	path := filepath.Join(t.TempDir(), "img.pdf")
	os.WriteFile(path, pdf, 0o600)
	out, err := exec.Command("pdfinfo", path).CombinedOutput()
	if err != nil || strings.Contains(string(out), "Error") {
		t.Fatalf("pdfinfo: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if size, ok := strings.CutPrefix(line, "Page size:"); ok {
			return strings.TrimSpace(size)
		}
	}
	t.Fatalf("no page size in:\n%s", out)
	return ""
}

func TestImageToPDF(t *testing.T) {
	var transparent bytes.Buffer
	png.Encode(&transparent, image.NewNRGBA(image.Rect(0, 0, 30, 60)))
	gray := image.NewGray(image.Rect(0, 0, 50, 20))
	gray.Set(1, 1, color.Gray{Y: 10})
	var grayJPEG bytes.Buffer
	jpeg.Encode(&grayJPEG, gray, nil)

	for _, tc := range []struct {
		name string
		data []byte
		page string
	}{
		{"portrait photo", testJPEG(t, 30, 60, 1), "595 x 842"},
		{"landscape photo", testJPEG(t, 60, 30, 1), "842 x 595"},
		{"landscape stored, shown portrait (phone camera)", testJPEG(t, 60, 30, 6), "595 x 842"},
		{"transparent PNG", transparent.Bytes(), "595 x 842"},
		{"grayscale JPEG", grayJPEG.Bytes(), "842 x 595"},
	} {
		pdf, err := imageToPDF(tc.data)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if size := pdfPageSize(t, pdf); !strings.HasPrefix(size, tc.page) {
			t.Errorf("%s: page %q, want %s", tc.name, size, tc.page)
		}
	}
	if _, err := imageToPDF([]byte("jpegdata")); err == nil {
		t.Error("a broken image converted without error")
	}
}
