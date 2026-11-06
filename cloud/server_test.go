package cloud

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

type client struct {
	t   *testing.T
	url string
	h   *http.Client
}

func newTestServer(t *testing.T) (*Store, *client) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv := httptest.NewServer(NewServer(store).Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return store, &client{t, srv.URL, &http.Client{Jar: jar}}
}

// do sends a dashboard request (with the header the web app sends) and
// decodes the reply into out; it returns the HTTP status.
func (c *client) do(method, path string, in, out any, header ...string) int {
	c.t.Helper()
	var body bytes.Buffer
	if in != nil {
		json.NewEncoder(&body).Encode(in)
	}
	req, _ := http.NewRequest(method, c.url+path, &body)
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-Requested-With", "print-cloud")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := c.h.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func TestLoginPairReport(t *testing.T) {
	store, c := newTestServer(t)
	if _, err := store.SetUser("Owner@Shop.com", "correct horse"); err != nil {
		t.Fatal(err)
	}
	if code := c.do("GET", "/api/agents", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("agents without login: %d", code)
	}
	if code := c.do("POST", "/api/login", map[string]string{"email": "owner@shop.com", "password": "wrong password"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
	if code := c.do("POST", "/api/login", map[string]string{"email": "owner@shop.com", "password": "correct horse"}, nil); code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}

	var pc struct{ Code string }
	c.do("POST", "/api/pairing-code", nil, &pc)
	if len(pc.Code) != 6 {
		t.Fatalf("pairing code %q", pc.Code)
	}
	var paired struct {
		AgentID string `json:"agent_id"`
		Token   string `json:"token"`
	}
	if code := c.do("POST", "/agent/v1/pair", map[string]string{"code": pc.Code, "name": "Office PC"}, &paired); code != http.StatusOK {
		t.Fatalf("pair: %d", code)
	}
	if code := c.do("POST", "/agent/v1/pair", map[string]string{"code": pc.Code, "name": "Again"}, nil); code != http.StatusNotFound {
		t.Fatalf("a pairing code works only once, got %d", code)
	}

	auth := []string{"Authorization", "Bearer " + paired.Token}
	report := map[string]any{
		"printer": "HP_M126nw", "printer_state": "ready",
		"requests": []map[string]any{
			{"id": "r2", "at": time.Now(), "status": "printed", "subject": "Invoice"},
			{"id": "r1", "at": time.Now().Add(-time.Hour), "status": "waiting", "subject": "Receipt"},
		},
	}
	var out struct{ Commands []Command }
	if code := c.do("POST", "/agent/v1/report", report, &out, auth...); code != http.StatusOK || len(out.Commands) != 0 {
		t.Fatalf("report: %d %+v", code, out)
	}

	var list []Agent
	c.do("GET", "/api/agents", nil, &list)
	if len(list) != 1 || !list[0].Online || list[0].Name != "Office PC" || list[0].Counts != (Counts{Printed: 1, Waiting: 1}) {
		t.Fatalf("agents = %+v", list)
	}
	var detail Agent
	c.do("GET", "/api/agents/"+paired.AgentID, nil, &detail)
	if len(detail.Requests) != 2 || !bytes.Contains(detail.Report, []byte("HP_M126nw")) || bytes.Contains(detail.Report, []byte("Invoice")) {
		t.Fatalf("detail = %+v / %s", detail, detail.Report)
	}

	// A report without "requests" keeps the history.
	c.do("POST", "/agent/v1/report", map[string]any{"printer": "HP_M126nw"}, nil, auth...)
	c.do("GET", "/api/agents/"+paired.AgentID, nil, &detail)
	if len(detail.Requests) != 2 {
		t.Fatalf("history lost by a report without requests: %d", len(detail.Requests))
	}

	// Test print: queued until the agent reports it done.
	var cmd struct{ ID string }
	if code := c.do("POST", "/api/agents/"+paired.AgentID+"/test-print", nil, &cmd); code != http.StatusOK {
		t.Fatalf("test print: %d", code)
	}
	c.do("POST", "/agent/v1/report", map[string]any{}, &out, auth...)
	if len(out.Commands) != 1 || out.Commands[0].ID != cmd.ID || out.Commands[0].Kind != "test-print" {
		t.Fatalf("commands = %+v", out.Commands)
	}
	c.do("POST", "/agent/v1/report", map[string]any{"done": []string{cmd.ID}}, &out, auth...)
	if len(out.Commands) != 0 {
		t.Fatalf("command still pending after done: %+v", out.Commands)
	}

	// Removing the agent revokes its token.
	c.do("DELETE", "/api/agents/"+paired.AgentID, nil, nil)
	if code := c.do("POST", "/agent/v1/report", map[string]any{}, nil, auth...); code != http.StatusUnauthorized {
		t.Fatalf("report after removal: %d", code)
	}
}

func TestUsersSeeOnlyTheirAgents(t *testing.T) {
	store, c := newTestServer(t)
	store.SetUser("a@shop.com", "password-a")
	store.SetUser("b@shop.com", "password-b")
	var bID int64
	store.db.QueryRow(`SELECT id FROM users WHERE email = 'b@shop.com'`).Scan(&bID)
	code, _ := store.NewPairingCode(bID, time.Minute)
	agentID, _, err := store.Pair(code, "B's PC")
	if err != nil {
		t.Fatal(err)
	}
	c.do("POST", "/api/login", map[string]string{"email": "a@shop.com", "password": "password-a"}, nil)
	var list []Agent
	c.do("GET", "/api/agents", nil, &list)
	if len(list) != 0 {
		t.Fatalf("a sees %+v", list)
	}
	for _, req := range []struct{ method, path string }{
		{"GET", "/api/agents/" + agentID}, {"POST", "/api/agents/" + agentID + "/test-print"}, {"DELETE", "/api/agents/" + agentID},
	} {
		if code := c.do(req.method, req.path, nil, nil); code != http.StatusNotFound {
			t.Errorf("%s %s by another user: %d", req.method, req.path, code)
		}
	}
}

func TestWritesNeedAppHeader(t *testing.T) {
	store, c := newTestServer(t)
	store.SetUser("a@shop.com", "password-a")
	c.do("POST", "/api/login", map[string]string{"email": "a@shop.com", "password": "password-a"}, nil)
	req, _ := http.NewRequest("POST", c.url+"/api/pairing-code", nil)
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site style POST: %d", res.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, c := newTestServer(t)
	code := 0
	for range 11 {
		code = c.do("POST", "/api/login", map[string]string{"email": "x@y.com", "password": "nope-nope"}, nil)
	}
	if code != http.StatusTooManyRequests {
		t.Fatalf("11th wrong login: %d", code)
	}
}
