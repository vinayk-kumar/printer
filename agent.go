package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

const maxLogSize = 5 << 20

// runAgent is the background loop: check the mailbox, print, sleep, repeat.
// Settings are re-read on every round, so re-running install takes effect.
func runAgent() error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	if fi, err := os.Stat(logPath()); err == nil && fi.Size() > maxLogSize {
		_ = os.Rename(logPath(), logPath()+".old")
	}
	f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("Print Agent %s started", version)
	markUnfinished()
	agentHealth.Lock()
	agentHealth.started = time.Now()
	agentHealth.Unlock()

	lastErr := ""
	pageStarted := false
	var cloud cloudSync
	for {
		c, err := loadConfig()
		if err != nil {
			reportOnce(&lastErr, fmt.Sprintf("Not set up yet (%v). Run: print-agent install", err))
			time.Sleep(30 * time.Second)
			continue
		}
		if !pageStarted {
			if c.StatusKey == "" { // set up by an older version
				c.StatusKey = newStatusKey()
				_ = saveConfig(c)
			}
			startStatusPage(c)
			pageStarted = true
		}
		recoverPrinter(c)
		err = pollOnce(c, loadState())
		if err != nil {
			reportOnce(&lastErr, fmt.Sprintf("Mailbox check failed: %v", err))
		} else if lastErr != "" {
			log.Printf("Mailbox connection OK again")
			lastErr = ""
		}
		agentHealth.Lock()
		agentHealth.lastCheck = time.Now()
		agentHealth.lastError = ifThen(err != nil, lastErr)
		agentHealth.Unlock()
		cloud.report(c)
		time.Sleep(time.Duration(c.PollSeconds) * time.Second)
	}
}

// reportOnce logs an error only when it changes, so an outage doesn't flood the log.
func reportOnce(last *string, msg string) {
	if msg != *last {
		log.Print(msg)
		*last = msg
	}
}
