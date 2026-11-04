package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vinayk-kumar/printer/cloud"
)

// TestCloudReporting pairs the agent with a real dashboard server, reports,
// and runs a test print sent from the dashboard.
func TestCloudReporting(t *testing.T) {
	useTempConfig(t, &Config{Email: "p@gmail.com", PrinterName: "Office", StatusKey: "k"})
	got := capturePrints(t)
	jobFn = func(*Config, string) (bool, string) { return false, "" }
	t.Cleanup(func() { jobFn = jobState })

	store, err := cloud.OpenStore(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	srv := httptest.NewServer(cloud.NewServer(store).Handler())
	defer srv.Close()
	store.SetUser("owner@shop.com", "password-1")
	token, _ := store.Login("owner@shop.com", "password-1", time.Hour)
	userID, _, _ := store.SessionUser(token)
	code, _ := store.NewPairingCode(userID, time.Minute)

	c, _ := loadConfig()
	if err := pairCloud(c, "ftp://"+strings.TrimPrefix(srv.URL, "http://"), code); err == nil {
		t.Fatal("paired with a non-https address")
	}
	if err := pairCloud(c, srv.URL, code); err != nil { // httptest is on 127.0.0.1, allowed without https
		t.Fatal(err)
	}
	saveConfig(c)
	saveRequest(&printRequest{ID: "r1", At: time.Now(), From: "shop@example.com", Subject: "Order 42", Status: statusPrinted,
		Files: []printedFile{{Name: "invoice.pdf", Status: statusPrinted}}})

	var sync cloudSync
	sync.report(c)
	agents, _ := store.Agents(userID)
	if len(agents) != 1 || !agents[0].Online || agents[0].Counts.Printed != 1 {
		t.Fatalf("dashboard sees %+v", agents)
	}
	detail, _ := store.AgentDetail(userID, agents[0].ID)
	if len(detail.Requests) != 1 || !strings.Contains(string(detail.Requests[0]), "Order 42") ||
		!strings.Contains(string(detail.Report), `"printer":"Office"`) || strings.Contains(string(detail.Report), "Order 42") {
		t.Fatalf("detail: report %s, requests %s", detail.Report, detail.Requests)
	}

	// A test print requested on the dashboard prints once and is reported done.
	if _, err := store.AddCommand(userID, agents[0].ID, "test-print"); err != nil {
		t.Fatal(err)
	}
	sync.report(c) // receives the command and prints
	sync.report(c) // reports it done (and the new history)
	sync.report(c)
	if len(*got) != 1 || (*got)[0].name != "Print Agent test page" {
		t.Fatalf("printed %q, want one test page", *got)
	}
	if cmds, _ := store.PendingCommands(agents[0].ID); len(cmds) != 0 {
		t.Fatalf("command not done: %+v", cmds)
	}
	time.Sleep(50 * time.Millisecond) // the test page's follow-up saves its result
	sync.report(c)
	detail, _ = store.AgentDetail(userID, agents[0].ID)
	if len(detail.Requests) != 2 {
		t.Fatalf("test page not in the dashboard history: %s", detail.Requests)
	}

	// Removed on the dashboard: the agent stops reporting.
	store.RemoveAgent(userID, agents[0].ID)
	sync.report(c)
	if c2, _ := loadConfig(); c2.CloudToken != "" {
		t.Fatal("agent kept its token after removal")
	}
}
