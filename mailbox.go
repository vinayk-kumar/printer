package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset" // decode non-UTF-8 emails
	"github.com/emersion/go-message/mail"
)

var printable = map[string]bool{".pdf": true, ".jpg": true, ".jpeg": true, ".png": true}

// printFn is swapped out in tests so they don't need a printer.
var printFn = printFile

func connect(c *Config) (*client.Client, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	cl, err := client.DialWithDialerTLS(dialer, net.JoinHostPort(c.IMAPHost, "993"), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot reach mail server %s: %w", c.IMAPHost, err)
	}
	// Timeout so a network change can't hang the agent on a dead connection.
	cl.Timeout = 60 * time.Second
	if err := cl.Login(c.Email, c.Password); err != nil {
		cl.Logout()
		return nil, fmt.Errorf("login failed: %w", err)
	}
	return cl, nil
}

// checkMailbox logs in and returns the number of emails in the inbox.
func checkMailbox(c *Config) (uint32, error) {
	cl, err := connect(c)
	if err != nil {
		return 0, err
	}
	defer cl.Logout()
	mbox, err := cl.Select("INBOX", true)
	if err != nil {
		return 0, err
	}
	return mbox.Messages, nil
}

func pollOnce(c *Config, st map[string]mailboxState) error {
	cl, err := connect(c)
	if err != nil {
		return err
	}
	defer cl.Logout()
	mbox, err := cl.Select("INBOX", true) // read-only: emails stay unread
	if err != nil {
		return err
	}

	key := strings.ToLower(c.Email)
	s, known := st[key]
	if !known || s.UIDValidity != mbox.UidValidity {
		// First run for this mailbox: skip everything already in it.
		last := uint32(0)
		if mbox.UidNext > 0 {
			last = mbox.UidNext - 1
		}
		st[key] = mailboxState{UIDValidity: mbox.UidValidity, LastUID: last}
		log.Printf("Watching %s: ignoring %d emails already in the inbox", c.Email, mbox.Messages)
		return saveState(st)
	}
	if mbox.UidNext > 0 && mbox.UidNext <= s.LastUID+1 {
		return nil // nothing new
	}

	criteria := imap.NewSearchCriteria()
	criteria.Uid = new(imap.SeqSet)
	criteria.Uid.AddRange(s.LastUID+1, 0) // LastUID+1:*
	uids, err := cl.UidSearch(criteria)
	if err != nil {
		return err
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	for _, uid := range uids {
		if uid <= s.LastUID {
			continue // "n:*" always returns the newest email, even if older than n
		}
		if err := fetchAndPrint(cl, c, uid); err != nil {
			log.Printf("Email %d: %v", uid, err)
		}
		s.LastUID = uid
		st[key] = s
		if err := saveState(st); err != nil {
			return err
		}
	}
	return nil
}

func fetchAndPrint(cl *client.Client, c *Config, uid uint32) error {
	seq := new(imap.SeqSet)
	seq.AddNum(uid)
	section := &imap.BodySectionName{Peek: true}
	messages := make(chan *imap.Message, 1)
	if err := cl.UidFetch(seq, []imap.FetchItem{section.FetchItem()}, messages); err != nil {
		return err
	}
	msg := <-messages
	if msg == nil {
		return fmt.Errorf("email not found")
	}
	body := msg.GetBody(section)
	if body == nil {
		return fmt.Errorf("email has no body")
	}
	return printAttachments(c, body)
}

func printAttachments(c *Config, r io.Reader) error {
	mr, err := mail.CreateReader(r)
	if err != nil {
		return err
	}
	from := ""
	if addrs, err := mr.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		from = strings.ToLower(addrs[0].Address)
	}
	subject, _ := mr.Header.Subject()
	if len(c.AllowedSenders) > 0 && !contains(c.AllowedSenders, from) {
		log.Printf("Ignored email from %s (not an allowed sender)", from)
		return nil
	}

	printed := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := partFilename(part.Header)
		if name == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !printable[ext] {
			log.Printf("Skipped %s (only PDF, JPG and PNG are printed)", name)
			continue
		}
		if err := printPart(c, part.Body, name, ext); err != nil {
			log.Printf("Print failed for %s: %v", name, err)
			continue
		}
		log.Printf("Printed %s from %s (subject: %q)", name, from, subject)
		printed++
	}
	if printed == 0 {
		log.Printf("No printable attachment in email from %s (subject: %q)", from, subject)
	}
	return nil
}

func printPart(c *Config, body io.Reader, name, ext string) error {
	tmp, err := os.CreateTemp("", "print-agent-*"+ext)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	return printFn(c, tmp.Name(), name)
}

func partFilename(h mail.PartHeader) string {
	switch h := h.(type) {
	case *mail.AttachmentHeader:
		if name, err := h.Filename(); err == nil && name != "" {
			return name
		}
	case *mail.InlineHeader:
		// Some mail apps send files inline instead of as attachments.
		if _, params, err := h.ContentDisposition(); err == nil && params["filename"] != "" {
			return params["filename"]
		}
		if _, params, err := h.ContentType(); err == nil && params["name"] != "" {
			return params["name"]
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
