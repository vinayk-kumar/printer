package main

import (
	"bytes"
	"fmt"
	"strings"
)

// testPagePDF builds a minimal one-page A4 PDF with the given lines of text,
// so test printing needs no PDF library.
func testPagePDF(lines []string) []byte {
	esc := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`)
	var content bytes.Buffer
	content.WriteString("BT\n/F1 22 Tf\n72 760 Td\n")
	for i, line := range lines {
		if i == 1 {
			content.WriteString("/F1 13 Tf\n0 -12 Td\n")
		}
		fmt.Fprintf(&content, "(%s) Tj\n0 -24 Td\n", esc.Replace(line))
	}
	content.WriteString("ET\n")

	return buildPDF([][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"),
		pdfStream("", content.Bytes()),
		[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"),
	})
}

// pdfStream is a PDF stream object; dict holds extra entries besides /Length.
func pdfStream(dict string, data []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "<< %s/Length %d >>\nstream\n", dict, len(data))
	b.Write(data)
	b.WriteString("\nendstream")
	return b.Bytes()
}

// buildPDF writes objects 1..n (object 1 is the catalog) with their cross-reference table.
func buildPDF(objects [][]byte) []byte {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n", i+1)
		pdf.Write(obj)
		pdf.WriteString("\nendobj\n")
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return pdf.Bytes()
}
