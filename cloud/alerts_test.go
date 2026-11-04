package cloud

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAlerts(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	store.SetUser("owner@shop.com", "password-1")
	token, _ := store.Login("owner@shop.com", "password-1", time.Hour)
	userID, _, _ := store.SessionUser(token)
	code, _ := store.NewPairingCode(userID, time.Minute)
	agentID, _, _ := store.Pair(code, "Office PC")

	report := func(state, problem, mailboxErr string) {
		t.Helper()
		r, _ := json.Marshal(map[string]string{"printer": "HP_M126nw", "printer_state": state, "printer_problem": problem, "mailbox_error": mailboxErr})
		if err := store.SaveReport(agentID, r, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	titles := func() []string {
		t.Helper()
		list, _, err := store.Alerts(userID, 100)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for i := len(list) - 1; i >= 0; i-- { // oldest first
			out = append(out, list[i].Title)
		}
		return out
	}
	tick := func(d time.Duration) { clock = clock.Add(d) }

	report("ready", "", "")
	tick(15 * time.Second)
	report("problem", "Printer is off or not connected", "")
	tick(15 * time.Second)
	report("ready", "", "") // a 15-second blip: no alert
	if got := titles(); len(got) != 0 {
		t.Fatalf("blip alerted: %q", got)
	}

	report("problem", "Printer is off or not connected", "")
	tick(15 * time.Second)
	report("problem", "Printer is off or not connected", "")
	tick(15 * time.Second)
	report("problem", "Printer is off or not connected", "") // 30 s: alert once
	tick(15 * time.Second)
	report("problem", "Printer is off or not connected", "")
	tick(15 * time.Second)
	report("ready", "", "")

	tick(15 * time.Second)
	if err := store.CheckOffline(); err != nil {
		t.Fatal(err)
	}
	tick(3 * time.Minute) // no reports: offline
	store.CheckOffline()
	store.CheckOffline() // only once
	report("ready", "", "")

	want := []string{"HP_M126nw needs attention", "HP_M126nw is ready again", "Office PC is offline", "Office PC is back online"}
	got := titles()
	if len(got) != len(want) {
		t.Fatalf("alerts %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("alerts %q, want %q", got, want)
		}
	}
	if _, unread, _ := store.Alerts(userID, 100); unread != 4 {
		t.Fatalf("unread = %d", unread)
	}
	store.MarkAlertsRead(userID)
	if _, unread, _ := store.Alerts(userID, 100); unread != 0 {
		t.Fatalf("unread after reading = %d", unread)
	}
}
