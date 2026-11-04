package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testEmail = "From: Shop <Orders@Example.com>\r\n" +
	"To: printer@example.com\r\n" +
	"Subject: Order 42\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=b1\r\n" +
	"\r\n" +
	"--b1\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"Please print.\r\n" +
	"--b1\r\n" +
	"Content-Type: application/pdf; name=\"invoice.pdf\"\r\n" +
	"Content-Disposition: attachment; filename=\"invoice.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"JVBERi0xLjQKJSVFT0YK\r\n" +
	"--b1\r\n" +
	"Content-Type: application/zip; name=\"data.zip\"\r\n" +
	"Content-Disposition: attachment; filename=\"data.zip\"\r\n" +
	"\r\n" +
	"zipdata\r\n" +
	"--b1\r\n" +
	"Content-Type: image/jpeg; name=\"photo.JPG\"\r\n" +
	"Content-Disposition: inline\r\n" +
	"\r\n" +
	"jpegdata\r\n" +
	"--b1--\r\n"

type printed struct{ name, content string }

func capturePrints(t *testing.T) *[]printed {
	t.Helper()
	var got []printed
	orig := printFn
	printFn = func(c *Config, path, title string) (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, printed{title, string(data)})
		return "job-" + title, nil
	}
	t.Cleanup(func() { printFn = orig })
	return &got
}

func TestPrintsOnlyPrintableAttachments(t *testing.T) {
	got := capturePrints(t)
	r, err := printAttachments(&Config{}, strings.NewReader(testEmail))
	if err != nil {
		t.Fatal(err)
	}
	want := []printed{{"invoice.pdf", "%PDF-1.4\n%%EOF\n"}, {"photo.JPG", "jpegdata"}}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("printed %q, want %q", *got, want)
	}
	if r.From != "orders@example.com" || r.Subject != "Order 42" || r.Status != statusPrinting || len(r.Files) != 3 {
		t.Fatalf("request = %+v", r)
	}
	if f := r.Files[1]; f.Name != "data.zip" || f.Status != statusIgnored {
		t.Fatalf("zip file recorded as %+v, want skipped", f)
	}
}

func TestAllowedSenders(t *testing.T) {
	got := capturePrints(t)
	printAttachments(&Config{AllowedSenders: []string{"someone@else.com"}}, strings.NewReader(testEmail))
	if len(*got) != 0 {
		t.Fatalf("printed %q for a sender that is not allowed", *got)
	}
	printAttachments(&Config{AllowedSenders: splitList("someone@else.com, ORDERS@example.com")}, strings.NewReader(testEmail))
	if len(*got) != 2 {
		t.Fatalf("allowed sender (case-insensitive) printed %d files, want 2", len(*got))
	}
}

func TestGuessIMAPHost(t *testing.T) {
	for email, want := range map[string]string{
		"a@gmail.com":   "imap.gmail.com",
		"a@Zoho.in":     "imap.zoho.in",
		"a@company.com": "imap.company.com",
		"a@hotmail.com": "outlook.office365.com",
	} {
		if got := guessIMAPHost(email); got != want {
			t.Errorf("guessIMAPHost(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestTestPageIsValidPDF(t *testing.T) {
	pdf := testPagePDF([]string{"Title (1)", `Back\slash`})
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed")
	}
	path := filepath.Join(t.TempDir(), "test.pdf")
	os.WriteFile(path, pdf, 0o600)
	out, err := exec.Command("pdfinfo", path).CombinedOutput()
	if err != nil || !strings.Contains(strings.Join(strings.Fields(string(out)), " "), "Pages: 1") {
		t.Fatalf("pdfinfo: %v\n%s", err, out)
	}
	if text, _ := exec.Command("pdftotext", path, "-").Output(); !strings.Contains(string(text), `Back\slash`) {
		t.Fatalf("text not found in test page: %q", text)
	}
}
