package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"
)

// useTempConfig keeps tests away from the real settings and history.
func useTempConfig(t *testing.T, c *Config) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if c != nil {
		if err := saveConfig(c); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseLpstatPrinter(t *testing.T) {
	for _, tc := range []struct{ out, state, problem string }{
		{"printer HP is idle.  enabled since Tue\n\tForm mounted:\n\tAlerts: none\n", "ready", ""},
		{"printer HP now printing HP-12.  enabled since Tue\n\tForm mounted:\n\tAlerts: toner-low-warning\n", "printing", ""},
		{"printer Canon is idle.  enabled since Tue\n\tForm mounted:\n\tAlerts: media-empty-error\n", "problem", "Out of paper"},
		{"printer Epson now printing Epson-3.  enabled since Tue\n\tWaiting for printer to become available.\n\tForm mounted:\n", "problem", "Waiting for printer to become available"},
		{"printer Brother disabled since Tue -\n\tFilter failed\n\tAlerts: none\n", "problem", "Filter failed"},
		{"lpstat: Invalid destination name\n", "missing", "Not found on this computer"},
	} {
		state, problem := parseLpstatPrinter(tc.out)
		if state != tc.state || problem != tc.problem {
			t.Errorf("parseLpstatPrinter(%q) = %q, %q; want %q, %q", tc.out, state, problem, tc.state, tc.problem)
		}
	}
}

func TestWindowsPrinterHealth(t *testing.T) {
	for in, want := range map[string]string{"Normal": "ready", "Printing": "printing", "PaperOut": "problem", "Offline, Error": "problem"} {
		if state, _ := windowsPrinterHealth(in); state != want {
			t.Errorf("windowsPrinterHealth(%q) = %q, want %q", in, state, want)
		}
	}
	if _, problem := windowsPrinterHealth("PaperJam"); problem != "Paper jam" {
		t.Errorf("PaperJam problem = %q", problem)
	}
}

func TestIsAutomatic(t *testing.T) {
	h := func(k, v string) mail.Header {
		var m mail.Header
		if k != "" {
			m.Set(k, v)
		}
		return m
	}
	for _, tc := range []struct {
		h    mail.Header
		from string
		want bool
	}{
		{h("", ""), "shop@example.com", false},
		{h("", ""), "printer@gmail.com", true}, // our own mailbox
		{h("", ""), "no-reply@bank.com", true},
		{h("Auto-Submitted", "auto-generated"), "a@b.com", true},
		{h("Precedence", "bulk"), "a@b.com", true},
		{h("List-Id", "<news.b.com>"), "a@b.com", true},
	} {
		if got := isAutomatic(tc.h, tc.from, "printer@gmail.com"); got != tc.want {
			t.Errorf("isAutomatic(%s) = %v, want %v", tc.from, got, tc.want)
		}
	}
}

func TestReplyMessage(t *testing.T) {
	c := &Config{Email: "printer@gmail.com", PrinterName: "Office"}
	r := &printRequest{ID: "x1", From: "shop@example.com", Subject: "Order 42", Status: statusWaiting,
		messageID: "abc@example.com", Files: []printedFile{
			{Name: "invoice.pdf", Status: statusWaiting, Detail: "Out of paper"},
			{Name: "data.zip", Status: statusIgnored, Detail: "only PDF, JPG and PNG files are printed"},
		}}
	msg := string(replyMessage(c, r, time.Now()))
	for _, want := range []string{
		"To: shop@example.com\r\n", "Subject: Re: Order 42\r\n", "In-Reply-To: <abc@example.com>\r\n",
		"Auto-Submitted: auto-replied\r\n", "Not printed yet", "invoice.pdf: waiting (Out of paper)", "data.zip: skipped",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("reply has no %q:\n%s", want, msg)
		}
	}
	if smtpHost(&Config{IMAPHost: "imap.gmail.com"}) != "smtp.gmail.com" {
		t.Error("smtp host for Gmail")
	}
}

func TestFollowRequestRepliesWhenPrinted(t *testing.T) {
	useTempConfig(t, nil)
	polls := 0
	jobFn = func(c *Config, job string) (bool, string) {
		polls++
		if polls == 1 {
			return true, "Out of paper"
		}
		return false, ""
	}
	var replies []string
	replyFn = func(c *Config, r *printRequest) error {
		replies = append(replies, r.Status)
		return nil
	}
	followEvery = time.Millisecond
	t.Cleanup(func() { jobFn, replyFn, followEvery = jobState, sendStatusReply, 5*time.Second })

	r := &printRequest{ID: "r1", From: "shop@example.com", Files: []printedFile{{Name: "a.pdf", Status: statusPrinting, job: "j1"}}}
	r.settle()
	saveRequest(r)
	followRequest(&Config{}, r)

	if len(replies) != 1 || replies[0] != statusPrinted {
		t.Fatalf("replies = %v, want one reply saying printed", replies)
	}
	h := loadHistory()
	if len(h) != 1 || h[0].Status != statusPrinted || !h[0].Replied {
		t.Fatalf("history = %+v", h)
	}
}

func TestStatusPageNeedsKey(t *testing.T) {
	useTempConfig(t, &Config{Email: "p@gmail.com", PrinterName: "Office", StatusKey: "secret"})
	srv := httptest.NewServer(statusHandler())
	defer srv.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	res, _ := noRedirect.Get(srv.URL + "/api/status")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without key: %d", res.StatusCode)
	}
	res, _ = noRedirect.Get(srv.URL + "/?key=wrong")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", res.StatusCode)
	}
	res, _ = noRedirect.Get(srv.URL + "/?key=secret")
	if res.StatusCode != http.StatusSeeOther || len(res.Cookies()) != 1 {
		t.Fatalf("right key: %d, cookies %v", res.StatusCode, res.Cookies())
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/status", nil)
	req.AddCookie(res.Cookies()[0])
	res, _ = noRedirect.Do(req)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("with cookie: %d", res.StatusCode)
	}
}
