// Package cloud is the Print Agent cloud dashboard: agents on office PCs
// report their printer and print history here, and their owners check it from
// anywhere in a web app (PWA).
package cloud

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var errNotFound = errors.New("not found")

// dummyHash is checked when an email is unknown, so a login takes as long
// either way and doesn't tell which emails have accounts.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not a password"), bcrypt.DefaultCost)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pairing_codes (
	code_hash  TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS agents (
	id         TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL DEFAULT 0,
	report     TEXT NOT NULL DEFAULT '{}' -- latest status report, without requests
);
CREATE TABLE IF NOT EXISTS requests (
	agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
	id       TEXT NOT NULL,
	at       INTEGER NOT NULL,
	status   TEXT NOT NULL,
	body     TEXT NOT NULL,
	PRIMARY KEY (agent_id, id)
);
CREATE TABLE IF NOT EXISTS alert_state ( -- what was last alerted per agent
	agent_id         TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
	offline          INTEGER NOT NULL DEFAULT 0,
	printer_now      TEXT NOT NULL DEFAULT '',
	printer_since    INTEGER NOT NULL DEFAULT 0,
	printer_alerted  TEXT NOT NULL DEFAULT '',
	mailbox_now      TEXT NOT NULL DEFAULT '',
	mailbox_since    INTEGER NOT NULL DEFAULT 0,
	mailbox_alerted  TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS alerts (
	id       INTEGER PRIMARY KEY,
	user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	agent_id TEXT NOT NULL,
	level    TEXT NOT NULL, -- bad, warn or ok
	title    TEXT NOT NULL,
	message  TEXT NOT NULL,
	at       INTEGER NOT NULL,
	read_at  INTEGER
);
CREATE INDEX IF NOT EXISTS alerts_by_user ON alerts (user_id, id);
CREATE TABLE IF NOT EXISTS commands (
	id         TEXT PRIMARY KEY,
	agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	done_at    INTEGER
);
`

// Store keeps users, sessions, agents and their print history in SQLite.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite writes one at a time anyway; this avoids "database is locked"
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- Users and sessions

// SetUser adds a user, or changes the password of an existing one.
func (s *Store) SetUser(email, password string) (added bool, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return false, errors.New("not an email address")
	}
	if len(password) < 8 {
		return false, errors.New("the password must have at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE email = ?`, string(hash), email)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	_, err = s.db.Exec(`INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)`, email, string(hash), s.now().Unix())
	return err == nil, err
}

// Login checks the password and starts a session; it returns the session token.
func (s *Store) Login(email, password string, ttl time.Duration) (string, error) {
	var id int64
	var hash string
	err := s.db.QueryRow(`SELECT id, password_hash FROM users WHERE email = ?`, strings.ToLower(strings.TrimSpace(email))).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Same work as a wrong password, so timing doesn't tell which emails exist.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", errNotFound
	}
	token := randomToken()
	_, err = s.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), id, s.now().Add(ttl).Unix())
	return token, err
}

// SessionUser returns the user of a session token.
func (s *Store) SessionUser(token string) (id int64, email string, err error) {
	err = s.db.QueryRow(`SELECT u.id, u.email FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, hashToken(token), s.now().Unix()).Scan(&id, &email)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	return id, email, err
}

func (s *Store) Logout(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// ---- Pairing

// NewPairingCode returns a 6-digit code that pairs one agent with the user.
func (s *Store) NewPairingCode(userID int64, ttl time.Duration) (string, error) {
	now := s.now()
	_, _ = s.db.Exec(`DELETE FROM pairing_codes WHERE expires_at <= ?`, now.Unix())
	for range 5 {
		n, err := rand.Int(rand.Reader, big.NewInt(1000000))
		if err != nil {
			return "", err
		}
		code := fmt.Sprintf("%06d", n.Int64())
		_, err = s.db.Exec(`INSERT INTO pairing_codes (code_hash, user_id, expires_at) VALUES (?, ?, ?)`,
			hashToken(code), userID, now.Add(ttl).Unix())
		if err == nil {
			return code, nil
		}
	}
	return "", errors.New("could not make a pairing code")
}

// Pair uses up a pairing code and registers the agent; it returns the
// agent's id and its token.
func (s *Store) Pair(code, name string) (id, token string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var userID int64
	err = tx.QueryRow(`DELETE FROM pairing_codes WHERE code_hash = ? AND expires_at > ? RETURNING user_id`,
		hashToken(strings.TrimSpace(code)), s.now().Unix()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errNotFound
	}
	if err != nil {
		return "", "", err
	}
	id, token = randomToken()[:16], randomToken()
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Print Agent"
	}
	if _, err := tx.Exec(`INSERT INTO agents (id, user_id, name, token_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, userID, name, hashToken(token), s.now().Unix()); err != nil {
		return "", "", err
	}
	return id, token, tx.Commit()
}

// ---- Agents

// AgentByToken returns the agent's id for its token.
func (s *Store) AgentByToken(token string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM agents WHERE token_hash = ?`, hashToken(token)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	return id, err
}

// SaveReport stores an agent's status. requests is nil when the agent's
// history didn't change since its last report.
func (s *Store) SaveReport(agentID string, report json.RawMessage, requests []json.RawMessage, done []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().Unix()
	if _, err := tx.Exec(`UPDATE agents SET report = ?, last_seen = ? WHERE id = ?`, string(report), now, agentID); err != nil {
		return err
	}
	if requests != nil {
		if _, err := tx.Exec(`DELETE FROM requests WHERE agent_id = ?`, agentID); err != nil {
			return err
		}
		for _, body := range requests {
			var r struct {
				ID     string    `json:"id"`
				At     time.Time `json:"at"`
				Status string    `json:"status"`
			}
			if json.Unmarshal(body, &r) != nil || r.ID == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO requests (agent_id, id, at, status, body) VALUES (?, ?, ?, ?, ?)`,
				agentID, r.ID, r.At.Unix(), r.Status, string(body)); err != nil {
				return err
			}
		}
	}
	for _, id := range done {
		if _, err := tx.Exec(`UPDATE commands SET done_at = ? WHERE id = ? AND agent_id = ?`, now, id, agentID); err != nil {
			return err
		}
	}
	if err := s.alertOnReport(tx, agentID, report); err != nil {
		return err
	}
	return tx.Commit()
}

type Command struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// PendingCommands are the commands the agent hasn't done yet. Old ones
// expire, so a printer that was off for a day doesn't print stale test pages.
func (s *Store) PendingCommands(agentID string) ([]Command, error) {
	rows, err := s.db.Query(`SELECT id, kind FROM commands WHERE agent_id = ? AND done_at IS NULL AND created_at > ?
		ORDER BY created_at`, agentID, s.now().Add(-10*time.Minute).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Command{}
	for rows.Next() {
		var c Command
		if err := rows.Scan(&c.ID, &c.Kind); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// AddCommand queues a command for one of the user's agents.
func (s *Store) AddCommand(userID int64, agentID, kind string) (string, error) {
	if _, err := s.agentOf(userID, agentID); err != nil {
		return "", err
	}
	id := randomToken()[:16]
	_, err := s.db.Exec(`INSERT INTO commands (id, agent_id, kind, created_at) VALUES (?, ?, ?, ?)`, id, agentID, kind, s.now().Unix())
	return id, err
}

// Agent is an agent as the dashboard shows it.
type Agent struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	PairedAt time.Time         `json:"paired_at"`
	LastSeen *time.Time        `json:"last_seen"`
	Online   bool              `json:"online"`
	Report   json.RawMessage   `json:"report"`
	Counts   Counts            `json:"counts"`
	Requests []json.RawMessage `json:"requests,omitempty"`
}

// Counts are for the last 24 hours, except waiting, which is now.
type Counts struct {
	Printed int `json:"printed"`
	Waiting int `json:"waiting"`
	Failed  int `json:"failed"`
}

// An agent is offline when it hasn't reported for this long (it reports
// every mailbox check, 15 seconds by default).
const offlineAfter = 2 * time.Minute

func (s *Store) agentOf(userID int64, agentID string) (*Agent, error) {
	var a Agent
	var created, seen int64
	var report string
	err := s.db.QueryRow(`SELECT id, name, created_at, last_seen, report FROM agents WHERE id = ? AND user_id = ?`,
		agentID, userID).Scan(&a.ID, &a.Name, &created, &seen, &report)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	s.fill(&a, created, seen, report)
	return &a, nil
}

func (s *Store) fill(a *Agent, created, seen int64, report string) {
	a.PairedAt = time.Unix(created, 0).UTC()
	if seen > 0 {
		t := time.Unix(seen, 0).UTC()
		a.LastSeen = &t
		a.Online = s.now().Sub(t) < offlineAfter
	}
	a.Report = json.RawMessage(report)
	since := s.now().Add(-24 * time.Hour).Unix()
	_ = s.db.QueryRow(`SELECT
		COUNT(*) FILTER (WHERE status = 'printed' AND at > ?),
		COUNT(*) FILTER (WHERE status IN ('printing', 'waiting')),
		COUNT(*) FILTER (WHERE status = 'failed' AND at > ?)
		FROM requests WHERE agent_id = ?`, since, since, a.ID).Scan(&a.Counts.Printed, &a.Counts.Waiting, &a.Counts.Failed)
}

// Agents lists the user's agents.
func (s *Store) Agents(userID int64) ([]Agent, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at, last_seen, report FROM agents WHERE user_id = ? ORDER BY name, created_at`, userID)
	if err != nil {
		return nil, err
	}
	type row struct {
		a             Agent
		created, seen int64
		report        string
	}
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.a.ID, &r.a.Name, &r.created, &r.seen, &r.report); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, r)
	}
	rows.Close()
	list := []Agent{}
	for _, r := range found {
		s.fill(&r.a, r.created, r.seen, r.report)
		list = append(list, r.a)
	}
	return list, nil
}

// AgentDetail is one of the user's agents with its print history.
func (s *Store) AgentDetail(userID int64, agentID string) (*Agent, error) {
	a, err := s.agentOf(userID, agentID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT body FROM requests WHERE agent_id = ? ORDER BY at DESC, id DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	a.Requests = []json.RawMessage{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		a.Requests = append(a.Requests, json.RawMessage(body))
	}
	return a, rows.Err()
}

func (s *Store) RenameAgent(userID int64, agentID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("the name is empty")
	}
	res, err := s.db.Exec(`UPDATE agents SET name = ? WHERE id = ? AND user_id = ?`, name, agentID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

// RemoveAgent deletes the agent; its token stops working.
func (s *Store) RemoveAgent(userID int64, agentID string) error {
	res, err := s.db.Exec(`DELETE FROM agents WHERE id = ? AND user_id = ?`, agentID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}
