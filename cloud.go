package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// The cloud dashboard (print-cloud): a paired agent reports its printer and
// print history there on every mailbox check, so its owner can see it from
// anywhere, and picks up commands (test print) sent from the dashboard.

var cloudClient = &http.Client{Timeout: 10 * time.Second}

// cmdPair pairs this agent with a cloud dashboard: print-agent pair URL CODE.
func cmdPair(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: print-agent pair https://your-dashboard 123456 (the code is shown under 'Add a printer')")
	}
	c, err := loadConfig()
	if err != nil {
		return errors.New("set up the printer and mailbox first. Run: print-agent install")
	}
	if err := pairCloud(c, args[0], args[1]); err != nil {
		return err
	}
	if err := saveConfig(c); err != nil {
		return err
	}
	fmt.Printf("✓ Paired with %s. This printer now shows on the dashboard within 15 seconds.\n", c.CloudURL)
	if serviceStatus() != "running" {
		fmt.Println("  The background agent is not running; start it with: print-agent install")
	}
	return nil
}

func cmdUnpair() error {
	c, err := loadConfig()
	if err != nil {
		return errors.New("not set up yet. Run: print-agent install")
	}
	if c.CloudToken == "" {
		fmt.Println("Not paired with a cloud dashboard.")
		return nil
	}
	c.CloudURL, c.CloudToken = "", ""
	if err := saveConfig(c); err != nil {
		return err
	}
	fmt.Println("✓ Stopped reporting to the cloud dashboard (remove it there too).")
	return nil
}

// pairCloud uses a pairing code and stores the dashboard address and token in c.
func pairCloud(c *Config, base, code string) error {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not a web address", base)
	}
	if u.Scheme != "https" && !isLocalHost(u.Hostname()) {
		return errors.New("the dashboard address must start with https://")
	}
	name, _ := os.Hostname()
	var out struct {
		Token string `json:"token"`
	}
	if err := cloudCall(base+"/agent/v1/pair", "", map[string]string{"code": strings.TrimSpace(code), "name": name}, &out); err != nil {
		return fmt.Errorf("pairing failed: %w", err)
	}
	c.CloudURL, c.CloudToken = base, out.Token
	return nil
}

func isLocalHost(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

type cloudCommand struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// cloudReport is a status report: what the status page shows, plus the
// commands done since the last report. Requests is nil when the print
// history didn't change since the last report.
type cloudReport struct {
	statusResponse
	Requests *[]printRequest `json:"requests,omitempty"`
	Done     []string        `json:"done,omitempty"`
}

// cloudSync is the reporting state between rounds of the agent loop.
type cloudSync struct {
	synced      bool
	historySent time.Time       // modification time of the history last sent
	done        []string        // commands done, to report
	seen        map[string]bool // commands already run, so a retry doesn't print twice
	lastErr     string
}

// report sends the status to the dashboard and runs the commands it returns.
func (s *cloudSync) report(c *Config) {
	if c.CloudToken == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	// The time is read before the history, so a change in between is sent next time.
	var modTime time.Time
	if fi, err := os.Stat(historyPath()); err == nil {
		modTime = fi.ModTime()
	}
	r := cloudReport{statusResponse: statusView(c), Done: s.done}
	history := r.statusResponse.Requests
	r.statusResponse.Requests = nil
	sendHistory := !s.synced || !modTime.Equal(s.historySent)
	if sendHistory {
		r.Requests = &history
	}

	var out struct {
		Commands []cloudCommand `json:"commands"`
	}
	err := cloudCall(c.CloudURL+"/agent/v1/report", c.CloudToken, r, &out)
	var cerr *cloudError
	if errors.As(err, &cerr) && cerr.status == http.StatusUnauthorized {
		log.Printf("Removed from the cloud dashboard (%s); stopped reporting. Pair again with: print-agent pair", c.CloudURL)
		c.CloudURL, c.CloudToken = "", ""
		_ = saveConfig(c)
		return
	}
	if err != nil {
		reportOnce(&s.lastErr, fmt.Sprintf("Cloud dashboard not reachable: %v", err))
		return
	}
	if s.lastErr != "" {
		log.Printf("Cloud dashboard reachable again")
		s.lastErr = ""
	}
	s.done = nil
	if sendHistory {
		s.synced, s.historySent = true, modTime
	}
	for _, cmd := range out.Commands {
		if s.seen[cmd.ID] {
			continue
		}
		s.seen[cmd.ID] = true
		s.done = append(s.done, cmd.ID)
		switch cmd.Kind {
		case "test-print":
			log.Printf("Test page requested from the cloud dashboard")
			startTestPrint(c)
		default:
			log.Printf("Unknown command from the cloud dashboard: %q", cmd.Kind)
		}
	}
}

type cloudError struct {
	status int
	msg    string
}

func (e *cloudError) Error() string { return e.msg }

func cloudCall(endpoint, token string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "print-agent/"+version)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := cloudClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		return &cloudError{res.StatusCode, firstOf(e.Error, res.Status)}
	}
	return json.NewDecoder(res.Body).Decode(out)
}
