package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/vinayk-kumar/printer/internal/appicon"
)

// The status page: a small web app (installable as a PWA) served by the
// agent, to check from a phone on the same Wi-Fi, or on this computer,
// whether emails printed. It is opened with a secret link (the status key),
// which then stays in a cookie.

//go:embed web
var webFiles embed.FS

const keyCookie = "print_agent_key"

// agentHealth is what the background loop last saw, for the status page.
var agentHealth struct {
	sync.Mutex
	started   time.Time
	lastCheck time.Time
	lastError string
}

func newStatusKey() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// statusURL is the status page link for a phone on the same network.
func statusURL(c *Config) string {
	if c.StatusPort < 0 || c.StatusKey == "" {
		return ""
	}
	port := c.StatusPort
	if port == 0 {
		port = defaultStatusPort
	}
	return fmt.Sprintf("http://%s:%d/?key=%s", lanIP(), port, c.StatusKey)
}

// lanIP is this computer's address on the local network (no packet is sent).
func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func startStatusPage(c *Config) {
	if c.StatusPort < 0 {
		return
	}
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", c.StatusPort),
		Handler:           statusHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("Status page on port %d", c.StatusPort)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("Status page not available: %v", err)
		}
	}()
}

func statusHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, lockedPage)
			return
		}
		if r.URL.Query().Has("key") {
			http.Redirect(w, r, "/", http.StatusSeeOther) // keep the key out of the address bar
			return
		}
		serveWeb(w, "web/index.html", "text/html; charset=utf-8")
	})
	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		serveWeb(w, "web/manifest.webmanifest", "application/manifest+json")
	})
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		serveWeb(w, "web/sw.js", "text/javascript")
	})
	for _, size := range []int{180, 192, 512} {
		data := appicon.PNG(size)
		mux.HandleFunc(fmt.Sprintf("GET /icon-%d.png", size), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "max-age=86400")
			w.Write(data)
		})
	}
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			http.Error(w, `{"error":"open the status link again"}`, http.StatusUnauthorized)
			return
		}
		c, err := loadConfig()
		if err != nil {
			http.Error(w, `{"error":"not set up"}`, http.StatusServiceUnavailable)
			return
		}
		writeJSONResponse(w, statusView(c))
	})
	mux.HandleFunc("POST /api/test-print", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			http.Error(w, `{"error":"open the status link again"}`, http.StatusUnauthorized)
			return
		}
		c, err := loadConfig()
		if err != nil {
			http.Error(w, `{"error":"not set up"}`, http.StatusServiceUnavailable)
			return
		}
		writeJSONResponse(w, startTestPrint(c))
	})
	return mux
}

// startTestPrint prints a test page, records it in the history and follows it.
func startTestPrint(c *Config) *printRequest {
	req := &printRequest{ID: newRequestID(), At: time.Now(), Subject: "Test page", noReply: true}
	job, err := sendTestPage(c)
	if err != nil {
		req.Files = []printedFile{{Name: "Test page", Status: statusFailed, Detail: err.Error()}}
	} else {
		req.Files = []printedFile{{Name: "Test page", Status: statusPrinting, job: job}}
	}
	req.settle()
	saveRequest(req)
	go followRequest(c, req)
	return req
}

// authorized checks the status key, from the link (?key=) or the cookie it sets.
func authorized(w http.ResponseWriter, r *http.Request) bool {
	c, err := loadConfig()
	if err != nil || c.StatusKey == "" {
		return false
	}
	same := func(s string) bool { return subtle.ConstantTimeCompare([]byte(s), []byte(c.StatusKey)) == 1 }
	if key := r.URL.Query().Get("key"); key != "" && same(key) {
		http.SetCookie(w, &http.Cookie{Name: keyCookie, Value: key, Path: "/", MaxAge: 400 * 24 * 3600,
			HttpOnly: true, SameSite: http.SameSiteLaxMode})
		return true
	}
	ck, err := r.Cookie(keyCookie)
	return err == nil && same(ck.Value)
}

type statusResponse struct {
	Version        string         `json:"version"`
	Mailbox        string         `json:"mailbox"`
	Printer        string         `json:"printer"`
	PrinterIP      string         `json:"printer_ip,omitempty"`
	PrinterState   string         `json:"printer_state"`
	PrinterProblem string         `json:"printer_problem,omitempty"`
	Senders        []string       `json:"senders"`
	PollSeconds    int            `json:"poll_seconds"`
	Replies        bool           `json:"replies"`
	Started        time.Time      `json:"started"`
	LastCheck      *time.Time     `json:"last_check,omitempty"`
	MailboxError   string         `json:"mailbox_error,omitempty"`
	Requests       []printRequest `json:"requests"`
}

func statusView(c *Config) statusResponse {
	state, problem := printerHealth(c)
	v := statusResponse{
		Version: version, Mailbox: c.Email, Printer: c.PrinterName, PrinterIP: c.PrinterIP,
		PrinterState: state, PrinterProblem: problem, Senders: c.AllowedSenders,
		PollSeconds: c.PollSeconds, Replies: !c.NoReplies, Requests: loadHistory(),
	}
	if v.Senders == nil {
		v.Senders = []string{}
	}
	if v.Requests == nil {
		v.Requests = []printRequest{}
	}
	agentHealth.Lock()
	v.Started, v.MailboxError = agentHealth.started, agentHealth.lastError
	if !agentHealth.lastCheck.IsZero() {
		t := agentHealth.lastCheck
		v.LastCheck = &t
	}
	agentHealth.Unlock()
	return v
}

func serveWeb(w http.ResponseWriter, name, contentType string) {
	data, err := webFiles.ReadFile(name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func writeJSONResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

const lockedPage = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Print Agent</title>
<style>body{font:16px/1.5 system-ui,sans-serif;margin:0;padding:48px 16px;background:#f4f6fa;color:#0f172a;text-align:center}
div{max-width:420px;margin:auto;background:#fff;border-radius:16px;padding:28px 20px;box-shadow:0 1px 3px rgba(15,23,42,.08)}
code{background:#eef2f7;padding:2px 6px;border-radius:6px}</style></head>
<body><div><h1 style="font-size:20px;margin:0 0 8px">Open the status link</h1>
<p>This page needs the link with its key. On the computer that runs Print Agent, run
<code>print-agent status</code> and open the "Status page" link shown there.</p></div></body></html>`
