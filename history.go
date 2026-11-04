package main

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

// Print history: one record per email, shown on the status page and by
// "print-agent status", and sent back to the sender as a reply.

const maxHistory = 200

const (
	statusPrinting = "printing" // sent to the printer, not finished yet
	statusPrinted  = "printed"
	statusWaiting  = "waiting" // still in the printer queue, e.g. out of paper
	statusFailed   = "failed"
	statusIgnored  = "ignored" // nothing to print, or sender not allowed
)

type printRequest struct {
	ID         string        `json:"id"`
	At         time.Time     `json:"at"`
	From       string        `json:"from"`
	Subject    string        `json:"subject"`
	Status     string        `json:"status"`
	Note       string        `json:"note,omitempty"`
	Files      []printedFile `json:"files,omitempty"`
	Replied    bool          `json:"replied,omitempty"`
	ReplyError string        `json:"reply_error,omitempty"`

	messageID string // of the email, so the reply joins its thread
	noReply   bool   // automatic mail, a sender that isn't allowed, or a test page
}

type printedFile struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	job    string // print queue job, while it is followed
}

// settle sets the email's status from its files: the worst one wins.
func (r *printRequest) settle() {
	rank := map[string]int{statusPrinted: 1, statusPrinting: 2, statusWaiting: 3, statusFailed: 4}
	status := statusIgnored
	for _, f := range r.Files {
		if rank[f.Status] > rank[status] {
			status = f.Status
		}
	}
	r.Status = status
}

func (r *printRequest) pending() bool {
	for _, f := range r.Files {
		if f.Status == statusPrinting || f.Status == statusWaiting {
			return true
		}
	}
	return false
}

var historyMu sync.Mutex

func newRequestID() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }

// loadHistory returns the recorded emails, newest first.
func loadHistory() []printRequest {
	var list []printRequest
	if data, err := os.ReadFile(historyPath()); err == nil {
		_ = json.Unmarshal(data, &list)
	}
	return list
}

// saveRequest adds the email to the history, or updates it.
func saveRequest(r *printRequest) {
	historyMu.Lock()
	defer historyMu.Unlock()
	list := loadHistory()
	found := false
	for i := range list {
		if list[i].ID == r.ID {
			list[i], found = *r, true
		}
	}
	if !found {
		list = append([]printRequest{*r}, list...)
	}
	if len(list) > maxHistory {
		list = list[:maxHistory]
	}
	if err := writeJSON(historyPath(), list); err != nil {
		log.Printf("Could not save print history: %v", err)
	}
}

// markUnfinished records that jobs followed before a restart have an
// unknown result.
func markUnfinished() {
	historyMu.Lock()
	defer historyMu.Unlock()
	list := loadHistory()
	changed := false
	for i := range list {
		for j := range list[i].Files {
			if f := &list[i].Files[j]; f.Status == statusPrinting {
				f.Status, f.Detail = statusWaiting, "Result unknown: the agent restarted while printing"
				changed = true
			}
		}
		list[i].settle()
	}
	if changed {
		_ = writeJSON(historyPath(), list)
	}
}

// Following a print job; variables so tests can shorten them.
var (
	jobFn       = jobState
	replyFn     = sendStatusReply
	followEvery = 5 * time.Second
	replyAfter  = 2 * time.Minute // reply "not printed yet" if a job is still queued by then
	followFor   = 30 * time.Minute
)

// followRequest waits for the email's print jobs to finish, records each
// change and replies to the sender once the result is known.
func followRequest(c *Config, r *printRequest) {
	start := time.Now()
	for {
		changed := false
		for i := range r.Files {
			f := &r.Files[i]
			if f.Status != statusPrinting && f.Status != statusWaiting {
				continue
			}
			status, detail := statusPrinted, ""
			if busy, problem := jobFn(c, f.job); busy {
				status, detail = statusPrinting, ""
				if problem != "" {
					status, detail = statusWaiting, problem
				} else if time.Since(start) >= replyAfter {
					status, detail = statusWaiting, "Still in the printer queue"
				}
			}
			if status != f.Status || detail != f.Detail {
				f.Status, f.Detail, changed = status, detail, true
				switch status {
				case statusPrinted:
					log.Printf("Printed %s from %s", f.Name, r.From)
				case statusWaiting:
					log.Printf("Not printed yet: %s (%s)", f.Name, detail)
				}
			}
		}
		done := !r.pending()
		if changed {
			r.settle()
		}
		if !r.Replied && !r.noReply && (done || time.Since(start) >= replyAfter) {
			r.Replied, changed = true, true
			if err := replyFn(c, r); err != nil {
				r.ReplyError = err.Error()
				log.Printf("Could not send the status reply to %s: %v", r.From, err)
			}
		}
		if changed {
			saveRequest(r)
		}
		if done || time.Since(start) >= followFor {
			return
		}
		time.Sleep(followEvery)
	}
}
