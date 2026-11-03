// Print Agent: prints PDF/JPG/PNG attachments of emails sent to a mailbox
// on a network printer. Runs in the background on Windows, macOS and Linux.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

const usage = `Print Agent %s - emails to a mailbox are printed on your network printer.

Usage:
  print-agent install      Set up mailbox + printer and start in the background
                           (run again to change settings)
  print-agent status       Show settings, printer and recent activity
  print-agent test-print   Print a test page
  print-agent uninstall    Stop and remove the agent (--purge also deletes settings)
  print-agent run          Run in the foreground (used by the background service)
  print-agent version

Install options (otherwise you are asked):
  --email EMAIL            Mailbox that receives the print emails
  --password PASSWORD      App password (or set PRINT_AGENT_PASSWORD)
  --printer-ip IP          Printer IP address, e.g. 192.168.1.50
  --imap-host HOST         Mail server (auto-detected for Gmail, Yahoo, Zoho)
  --allowed-senders LIST   Comma-separated senders allowed to print (default: anyone)
  --poll-seconds N         How often to check the mailbox (default 15)
  --yes                    Don't ask anything; fail if a value is missing
`

func main() {
	if len(os.Args) < 2 {
		fmt.Printf(usage, version)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "install":
		err = cmdInstall(os.Args[2:])
	case "run":
		err = runAgent()
	case "status":
		err = cmdStatus()
	case "test-print":
		err = cmdTestPrint()
	case "uninstall":
		err = cmdUninstall(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Printf(usage, version)
	default:
		fmt.Printf(usage, version)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
}
