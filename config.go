package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Email          string   `json:"email"`
	Password       string   `json:"password"`
	IMAPHost       string   `json:"imap_host"`
	PrinterIP      string   `json:"printer_ip"`
	PrinterName    string   `json:"printer_name"` // OS print queue created for PrinterIP
	AllowedSenders []string `json:"allowed_senders"`
	PollSeconds    int      `json:"poll_seconds"`
}

// mailboxState remembers the last email handled, so each email prints once
// and emails already in the inbox at setup time are never printed.
type mailboxState struct {
	UIDValidity uint32 `json:"uid_validity"`
	LastUID     uint32 `json:"last_uid"`
}

var imapHosts = map[string]string{
	"gmail.com":      "imap.gmail.com",
	"googlemail.com": "imap.gmail.com",
	"yahoo.com":      "imap.mail.yahoo.com",
	"yahoo.in":       "imap.mail.yahoo.com",
	"zoho.com":       "imap.zoho.com",
	"zoho.in":        "imap.zoho.in",
	"outlook.com":    "outlook.office365.com",
	"hotmail.com":    "outlook.office365.com",
	"live.com":       "outlook.office365.com",
}

func guessIMAPHost(email string) string {
	domain := strings.ToLower(email[strings.LastIndex(email, "@")+1:])
	if host, ok := imapHosts[domain]; ok {
		return host
	}
	return "imap." + domain
}

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "print-agent")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }
func statePath() string  { return filepath.Join(configDir(), "state.json") }
func logPath() string    { return filepath.Join(configDir(), "agent.log") }

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.PollSeconds < 5 {
		c.PollSeconds = 15
	}
	return &c, nil
}

func saveConfig(c *Config) error { return writeJSON(configPath(), c) }

func loadState() map[string]mailboxState {
	st := map[string]mailboxState{}
	if data, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func saveState(st map[string]mailboxState) error { return writeJSON(statePath(), st) }

// writeJSON writes atomically and readable by the current user only
// (the config holds the mailbox app password).
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
