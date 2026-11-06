package cloud

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vinayk-kumar/printer/internal/appicon"
)

//go:embed web
var webFiles embed.FS

const (
	sessionCookie = "print_cloud_session"
	sessionTTL    = 30 * 24 * time.Hour
	pairingTTL    = 10 * time.Minute
	maxReport     = 4 << 20
)

// Server serves the dashboard web app, its API (/api/) and the agent API
// (/agent/v1/).
type Server struct {
	store   *Store
	limiter *limiter
}

func NewServer(store *Store) *Server {
	return &Server{store: store, limiter: newLimiter(10, 10*time.Minute)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page("web/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /manifest.webmanifest", s.page("web/manifest.webmanifest", "application/manifest+json"))
	mux.HandleFunc("GET /sw.js", s.page("web/sw.js", "text/javascript"))
	for _, size := range []int{180, 192, 512} {
		data := appicon.PNG(size)
		mux.HandleFunc("GET /icon-"+strconv.Itoa(size)+".png", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "max-age=86400")
			w.Write(data)
		})
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok\n") })

	// Dashboard
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		writeJSON(w, http.StatusOK, map[string]string{"email": u.email})
	}))
	mux.HandleFunc("GET /api/agents", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		list, err := s.store.Agents(u.id)
		s.reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/agents/{id}", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		a, err := s.store.AgentDetail(u.id, r.PathValue("id"))
		s.reply(w, a, err)
	}))
	mux.HandleFunc("PATCH /api/agents/{id}", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		var in struct {
			Name string `json:"name"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		s.reply(w, map[string]bool{"ok": true}, s.store.RenameAgent(u.id, r.PathValue("id"), in.Name))
	}))
	mux.HandleFunc("DELETE /api/agents/{id}", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		s.reply(w, map[string]bool{"ok": true}, s.store.RemoveAgent(u.id, r.PathValue("id")))
	}))
	mux.HandleFunc("POST /api/agents/{id}/test-print", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		id, err := s.store.AddCommand(u.id, r.PathValue("id"), "test-print")
		s.reply(w, map[string]string{"id": id}, err)
	}))
	mux.HandleFunc("GET /api/alerts", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		list, unread, err := s.store.Alerts(u.id, 100)
		s.reply(w, map[string]any{"alerts": list, "unread": unread}, err)
	}))
	mux.HandleFunc("POST /api/alerts/read", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		s.reply(w, map[string]bool{"ok": true}, s.store.MarkAlertsRead(u.id))
	}))
	mux.HandleFunc("POST /api/pairing-code", s.user(func(w http.ResponseWriter, r *http.Request, u user) {
		code, err := s.store.NewPairingCode(u.id, pairingTTL)
		s.reply(w, map[string]any{"code": code, "expires_in": int(pairingTTL.Seconds())}, err)
	}))

	// Agents
	mux.HandleFunc("POST /agent/v1/pair", s.pair)
	mux.HandleFunc("POST /agent/v1/report", s.report)
	return securityHeaders(mux)
}

type user struct {
	id    int64
	email string
}

// user wraps a dashboard handler that needs a logged-in user.
func (s *Server) user(h func(http.ResponseWriter, *http.Request, user)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Please log in")
			return
		}
		id, email, err := s.store.SessionUser(ck.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Please log in")
			return
		}
		// Writes must be JSON: a form on another site can't send that, so
		// together with the SameSite cookie this blocks cross-site requests.
		if r.Method != http.MethodGet && r.ContentLength > 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "Send JSON")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Requested-With") != "print-cloud" {
			writeError(w, http.StatusForbidden, "Missing request header")
			return
		}
		h(w, r, user{id, email})
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.allow("login:" + ip) {
		writeError(w, http.StatusTooManyRequests, "Too many tries. Wait 10 minutes and try again.")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	token, err := s.store.Login(in.Email, in.Password, sessionTTL)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusUnauthorized, "Wrong email or password")
		return
	}
	if err != nil {
		s.reply(w, nil, err)
		return
	}
	s.limiter.reset("login:" + ip)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]string{"email": strings.ToLower(strings.TrimSpace(in.Email))})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.Logout(ck.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.allow("pair:" + ip) {
		writeError(w, http.StatusTooManyRequests, "too many tries; wait 10 minutes")
		return
	}
	var in struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id, token, err := s.store.Pair(in.Code, in.Name)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "wrong or expired pairing code; make a new one in the dashboard")
		return
	}
	if err != nil {
		s.reply(w, nil, err)
		return
	}
	s.limiter.reset("pair:" + ip)
	writeJSON(w, http.StatusOK, map[string]string{"agent_id": id, "token": token})
}

// report receives an agent's status and returns the commands waiting for it.
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	agentID, err := s.store.AgentByToken(token)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusUnauthorized, "this agent was removed from the dashboard")
		return
	}
	if err != nil {
		s.reply(w, nil, err)
		return
	}
	var in map[string]json.RawMessage
	if !readJSON(w, r, &in) {
		return
	}
	var requests []json.RawMessage
	if raw, ok := in["requests"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &requests); err != nil {
			writeError(w, http.StatusBadRequest, "bad requests list")
			return
		}
		if requests == nil {
			requests = []json.RawMessage{}
		}
	}
	var done []string
	if raw, ok := in["done"]; ok {
		_ = json.Unmarshal(raw, &done)
	}
	delete(in, "requests")
	delete(in, "done")
	report, _ := json.Marshal(in)
	if err := s.store.SaveReport(agentID, report, requests, done); err != nil {
		s.reply(w, nil, err)
		return
	}
	cmds, err := s.store.PendingCommands(agentID)
	s.reply(w, map[string]any{"commands": cmds}, err)
}

func (s *Server) page(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := webFiles.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	}
}

// reply writes v, or the error: not found as 404, anything else as 500.
func (s *Server) reply(w http.ResponseWriter, v any, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case err != nil:
		log.Printf("error: %v", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong")
	default:
		writeJSON(w, http.StatusOK, v)
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReport)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

// isHTTPS is true behind an HTTPS proxy (Caddy) too.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// clientIP is the caller's address. Behind Caddy on the same machine, it is
// the first address in X-Forwarded-For.
func clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			return strings.TrimSpace(strings.Split(fwd, ",")[0])
		}
	}
	return host
}

// limiter allows a few tries per key (e.g. logins per IP) in a time window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	tries  map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, tries: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var recent []time.Time
	for _, t := range l.tries[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.tries[key] = recent
		return false
	}
	l.tries[key] = append(recent, now)
	if len(l.tries) > 10000 { // don't grow forever under a flood of addresses
		l.tries = map[string][]time.Time{}
	}
	return true
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.tries, key)
	l.mu.Unlock()
}
