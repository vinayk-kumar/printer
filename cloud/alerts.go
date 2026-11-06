package cloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// Alerts tell the owner when an office PC goes offline, its printer is off
// or needs attention, or its mailbox can't be checked, and when that is
// fixed. A problem must last a little while before it is alerted, so a short
// blip doesn't raise an alarm.

const (
	printerAlertAfter = 30 * time.Second
	mailboxAlertAfter = 2 * time.Minute
	maxAlerts         = 500 // per user
)

type Alert struct {
	ID      int64     `json:"id"`
	AgentID string    `json:"agent_id"`
	Level   string    `json:"level"` // bad, warn or ok
	Title   string    `json:"title"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
	Read    bool      `json:"read"`
}

type alertState struct {
	offline                    bool
	printerNow, printerAlerted string
	printerSince               int64
	mailboxNow, mailboxAlerted string
	mailboxSince               int64
}

func loadAlertState(tx *sql.Tx, agentID string) (alertState, error) {
	var st alertState
	err := tx.QueryRow(`SELECT offline, printer_now, printer_since, printer_alerted, mailbox_now, mailbox_since, mailbox_alerted
		FROM alert_state WHERE agent_id = ?`, agentID).Scan(&st.offline, &st.printerNow, &st.printerSince, &st.printerAlerted,
		&st.mailboxNow, &st.mailboxSince, &st.mailboxAlerted)
	if err == sql.ErrNoRows {
		err = nil
	}
	return st, err
}

func saveAlertState(tx *sql.Tx, agentID string, st alertState) error {
	_, err := tx.Exec(`INSERT INTO alert_state (agent_id, offline, printer_now, printer_since, printer_alerted, mailbox_now, mailbox_since, mailbox_alerted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (agent_id) DO UPDATE SET offline = excluded.offline, printer_now = excluded.printer_now,
			printer_since = excluded.printer_since, printer_alerted = excluded.printer_alerted, mailbox_now = excluded.mailbox_now,
			mailbox_since = excluded.mailbox_since, mailbox_alerted = excluded.mailbox_alerted`,
		agentID, st.offline, st.printerNow, st.printerSince, st.printerAlerted, st.mailboxNow, st.mailboxSince, st.mailboxAlerted)
	return err
}

func (s *Store) addAlert(tx *sql.Tx, agentID, level, title, message string) error {
	var userID int64
	if err := tx.QueryRow(`SELECT user_id FROM agents WHERE id = ?`, agentID).Scan(&userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO alerts (user_id, agent_id, level, title, message, at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, agentID, level, title, message, s.now().Unix()); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM alerts WHERE user_id = ? AND id <= (SELECT id FROM alerts WHERE user_id = ? ORDER BY id DESC LIMIT 1 OFFSET ?)`,
		userID, userID, maxAlerts)
	return err
}

// alertOnReport compares an agent's new report with what was alerted before.
func (s *Store) alertOnReport(tx *sql.Tx, agentID string, report json.RawMessage) error {
	var r struct {
		Printer        string `json:"printer"`
		PrinterState   string `json:"printer_state"`
		PrinterProblem string `json:"printer_problem"`
		MailboxError   string `json:"mailbox_error"`
	}
	_ = json.Unmarshal(report, &r)
	var name string
	if err := tx.QueryRow(`SELECT name FROM agents WHERE id = ?`, agentID).Scan(&name); err != nil {
		return err
	}
	st, err := loadAlertState(tx, agentID)
	if err != nil {
		return err
	}
	now := s.now().Unix()

	if st.offline {
		st.offline = false
		if err := s.addAlert(tx, agentID, "ok", name+" is back online", "Print Agent is reporting again. Emails that arrived meanwhile print now."); err != nil {
			return err
		}
	}

	printer := firstNonEmpty(r.Printer, "The printer")
	problem := ""
	if r.PrinterState == "problem" || r.PrinterState == "missing" {
		problem = firstNonEmpty(r.PrinterProblem, "The printer needs attention")
	}
	switch watch(&st.printerNow, &st.printerSince, &st.printerAlerted, problem, printerAlertAfter, now) {
	case "problem":
		err = s.addAlert(tx, agentID, "warn", printer+" needs attention", problem+". Emails wait and print when it's fixed.")
	case "fixed":
		err = s.addAlert(tx, agentID, "ok", printer+" is ready again", "The printer is working on "+name+".")
	}
	if err != nil {
		return err
	}

	switch watch(&st.mailboxNow, &st.mailboxSince, &st.mailboxAlerted, r.MailboxError, mailboxAlertAfter, now) {
	case "problem":
		err = s.addAlert(tx, agentID, "warn", name+" can't check the mailbox", r.MailboxError)
	case "fixed":
		err = s.addAlert(tx, agentID, "ok", name+" is checking the mailbox again", "New emails print again.")
	}
	if err != nil {
		return err
	}
	return saveAlertState(tx, agentID, st)
}

// watch follows one problem ("" = none) and tells when to alert: "problem"
// once it has lasted `after` (or became a different problem), "fixed" when an
// alerted problem is gone.
func watch(current *string, since *int64, alerted *string, problem string, after time.Duration, now int64) string {
	if problem != *current {
		*current, *since = problem, now
	}
	switch {
	case problem == "" && *alerted != "":
		*alerted = ""
		return "fixed"
	case problem != "" && problem != *alerted && now-*since >= int64(after.Seconds()):
		*alerted = problem
		return "problem"
	}
	return ""
}

// CheckOffline alerts for agents that stopped reporting.
func (s *Store) CheckOffline() error {
	cutoff := s.now().Add(-offlineAfter).Unix()
	rows, err := s.db.Query(`SELECT a.id, a.name FROM agents a LEFT JOIN alert_state st ON st.agent_id = a.id
		WHERE a.last_seen > 0 AND a.last_seen < ? AND COALESCE(st.offline, 0) = 0`, cutoff)
	if err != nil {
		return err
	}
	type agent struct{ id, name string }
	var gone []agent
	for rows.Next() {
		var a agent
		if err := rows.Scan(&a.id, &a.name); err != nil {
			rows.Close()
			return err
		}
		gone = append(gone, a)
	}
	rows.Close()
	for _, a := range gone {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		st, err := loadAlertState(tx, a.id)
		if err == nil {
			st.offline = true
			err = saveAlertState(tx, a.id, st)
		}
		if err == nil {
			err = s.addAlert(tx, a.id, "bad", a.name+" is offline",
				"Print Agent stopped reporting. The computer may be off, asleep or without internet. Emails wait in the mailbox and print when it's back.")
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return nil
}

// WatchOffline runs CheckOffline every 30 seconds until ctx ends.
func (s *Store) WatchOffline(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if err := s.CheckOffline(); err != nil {
			log.Printf("offline check: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Alerts returns the user's newest alerts and how many are unread.
func (s *Store) Alerts(userID int64, limit int) ([]Alert, int, error) {
	var unread int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&unread); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id, agent_id, level, title, message, at, read_at IS NOT NULL FROM alerts
		WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := []Alert{}
	for rows.Next() {
		var a Alert
		var at int64
		if err := rows.Scan(&a.ID, &a.AgentID, &a.Level, &a.Title, &a.Message, &at, &a.Read); err != nil {
			return nil, 0, err
		}
		a.At = time.Unix(at, 0).UTC()
		list = append(list, a)
	}
	return list, unread, rows.Err()
}

func (s *Store) MarkAlertsRead(userID int64) error {
	_, err := s.db.Exec(`UPDATE alerts SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, s.now().Unix(), userID)
	return err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
