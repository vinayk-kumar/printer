package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
)

// Images are printed as a one-page PDF: printers handle PDF reliably, while
// some accept JPEG directly and then print nothing (e.g. HP LaserJet Pro
// MFP M126nw reports the job completed with 0 sheets). The image is fitted
// to an A4 page, turned the way the phone took it, with white behind
// transparent parts.

const (
	a4Width, a4Height = 595.0, 842.0
	pageMargin        = 28.0 // about 10 mm
)

func imageToPDF(data []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not a readable image: %w", err)
	}
	orientation := 1
	if format == "jpeg" {
		orientation = jpegOrientation(data)
	}

	// A plain RGB or grayscale JPEG goes into the PDF as it is; anything else
	// (PNG, CMYK JPEG) is redrawn on white and stored as a JPEG.
	img, colorSpace := data, "/DeviceRGB"
	switch {
	case format == "jpeg" && cfg.ColorModel == color.YCbCrModel:
	case format == "jpeg" && cfg.ColorModel == color.GrayModel:
		colorSpace = "/DeviceGray"
	default:
		src, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("not a readable image: %w", err)
		}
		rgb := image.NewRGBA(src.Bounds())
		draw.Draw(rgb, rgb.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(rgb, rgb.Bounds(), src, src.Bounds().Min, draw.Over)
		var b bytes.Buffer
		if err := jpeg.Encode(&b, rgb, &jpeg.Options{Quality: 92}); err != nil {
			return nil, err
		}
		img = b.Bytes()
	}

	// Size as shown: orientations 5-8 swap width and height.
	w, h := float64(cfg.Width), float64(cfg.Height)
	if orientation >= 5 {
		w, h = h, w
	}
	pageW, pageH := a4Width, a4Height
	if w > h {
		pageW, pageH = a4Height, a4Width // landscape page for a landscape image
	}
	scale := min((pageW-2*pageMargin)/w, (pageH-2*pageMargin)/h)
	dw, dh := w*scale, h*scale
	x, y := (pageW-dw)/2, (pageH-dh)/2

	content := fmt.Sprintf("q %s cm /Im1 Do Q", orientationMatrix(orientation, x, y, dw, dh))
	return buildPDF([][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>", pageW, pageH)),
		pdfStream("", []byte(content)),
		pdfStream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode ",
			cfg.Width, cfg.Height, colorSpace), img),
	}), nil
}

// orientationMatrix maps the image (a unit square in PDF) onto the box
// x, y, w, h as shown, turning or mirroring it per its EXIF orientation.
func orientationMatrix(orientation int, x, y, w, h float64) string {
	m := map[int][6]float64{
		1: {w, 0, 0, h, x, y},
		2: {-w, 0, 0, h, x + w, y},     // mirrored
		3: {-w, 0, 0, -h, x + w, y + h}, // upside down
		4: {w, 0, 0, -h, x, y + h},     // mirrored upside down
		5: {0, -h, -w, 0, x + w, y + h},
		6: {0, -h, w, 0, x, y + h}, // turn right to show
		7: {0, h, w, 0, x, y},
		8: {0, h, -w, 0, x + w, y}, // turn left to show
	}[orientation]
	if m == [6]float64{} {
		m = [6]float64{w, 0, 0, h, x, y}
	}
	return fmt.Sprintf("%.3f %.3f %.3f %.3f %.3f %.3f", m[0], m[1], m[2], m[3], m[4], m[5])
}

// jpegOrientation reads the EXIF orientation (1-8) of a JPEG; 1 when absent.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data starts: no EXIF before it
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return exifOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd+2 > len(tiff) {
		return 1
	}
	n := int(order.Uint16(tiff[ifd:]))
	for e := 0; e < n; e++ {
		off := ifd + 2 + e*12
		if off+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[off:]) == 0x0112 { // Orientation, a SHORT
			if v := int(order.Uint16(tiff[off+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
