package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
)

// sendStatusReply emails the sender what happened to their print request,
// from the printer's own mailbox (same app password, over SMTP).
func sendStatusReply(c *Config, r *printRequest) error {
	if c.NoReplies {
		return nil
	}
	host := smtpHost(c)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "587"), 30*time.Second)
	if err != nil {
		return fmt.Errorf("cannot reach mail server %s: %w", host, err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	cl, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if err := cl.StartTLS(&tls.Config{ServerName: host}); err != nil {
		return err
	}
	if err := cl.Auth(smtp.PlainAuth("", c.Email, c.Password, host)); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	if err := cl.Mail(c.Email); err != nil {
		return err
	}
	if err := cl.Rcpt(r.From); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(replyMessage(c, r, time.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

func replyMessage(c *Config, r *printRequest, now time.Time) []byte {
	subject := strings.TrimSpace(r.Subject)
	if subject == "" {
		subject = "Print status"
	} else if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", "Print Agent <"+c.Email+">")
	header("To", r.From)
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s.print-agent@%s>", r.ID, c.Email[strings.LastIndex(c.Email, "@")+1:]))
	if id := r.messageID; id != "" && !strings.ContainsAny(id, "<>\r\n ") {
		header("In-Reply-To", "<"+id+">")
		header("References", "<"+id+">")
	}
	// Tell mail servers this is an automatic reply, so they don't answer it.
	header("Auto-Submitted", "auto-replied")
	header("X-Auto-Response-Suppress", "All")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(replyText(c, r), "\n", "\r\n"))
	return b.Bytes()
}

func replyText(c *Config, r *printRequest) string {
	var b strings.Builder
	switch r.Status {
	case statusPrinted:
		b.WriteString("✓ Printed\n\nYour email was printed.\n")
	case statusWaiting, statusPrinting:
		b.WriteString("⏳ Not printed yet\n\nYour email is waiting at the printer. It prints as soon as the problem below is fixed.\n")
	case statusFailed:
		b.WriteString("✗ Not printed\n\nYour email could not be printed.\n")
	default:
		b.WriteString("Nothing to print\n\n")
		b.WriteString(firstOf(r.Note, "There was no PDF, JPG or PNG file attached.") + "\n")
	}
	if len(r.Files) > 0 {
		b.WriteString("\n")
	}
	for _, f := range r.Files {
		line := map[string]string{
			statusPrinted:  "✓ %s: printed",
			statusPrinting: "⏳ %s: printing",
			statusWaiting:  "⏳ %s: waiting",
			statusFailed:   "✗ %s: not printed",
			statusIgnored:  "– %s: skipped",
		}[f.Status]
		fmt.Fprintf(&b, "  "+line, f.Name)
		if f.Detail != "" {
			b.WriteString(" (" + f.Detail + ")")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\nPrinter: %s\nReceived: %s\n\nThis is an automatic reply from Print Agent.\n",
		c.PrinterName, r.At.Format("2 Jan 2006, 15:04"))
	return b.String()
}

// isAutomatic tells mail that must not get a reply: our own mail, bounces,
// newsletters and other automatic messages (so two robots never loop).
func isAutomatic(h mail.Header, from, own string) bool {
	if from == "" || strings.EqualFold(from, own) {
		return true
	}
	for _, s := range []string{"noreply", "no-reply", "donotreply", "do-not-reply", "mailer-daemon", "postmaster", "bounce"} {
		if strings.Contains(from, s) {
			return true
		}
	}
	if v := strings.ToLower(h.Get("Auto-Submitted")); v != "" && v != "no" {
		return true
	}
	switch strings.ToLower(h.Get("Precedence")) {
	case "bulk", "list", "junk", "auto_reply":
		return true
	}
	return h.Get("List-Id") != "" || h.Get("List-Unsubscribe") != ""
}
