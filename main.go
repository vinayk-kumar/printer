// Print Agent: prints PDF/JPG/PNG attachments of emails sent to a mailbox
// on any printer this computer can print to. Runs in the background on Windows, macOS and Linux.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

const usage = `Print Agent %s - emails to a mailbox are printed on your printer.

Usage:
  print-agent install      Set up mailbox + printer and start in the background
                           (run again to change settings)
  print-agent status       Show settings, printer and recent activity
  print-agent printers     List the printers installed on this computer
  print-agent test-print   Print a test page
  print-agent pair URL CODE  Show this printer on a cloud dashboard (print-cloud),
                           with the code from its "Add a printer" page
  print-agent unpair       Stop reporting to the cloud dashboard
  print-agent uninstall    Stop and remove the agent (--purge also deletes settings)
  print-agent run          Run in the foreground (used by the background service)
  print-agent version

Install options (otherwise you are asked):
  --email EMAIL            Mailbox that receives the print emails
  --password PASSWORD      App password (or set PRINT_AGENT_PASSWORD)
  --printer NAME           Use a printer installed on this computer (USB or network),
                           as listed by "print-agent printers"
  --printer-ip IP          Or: a network printer's IP address, e.g. 192.168.1.50
  --imap-host HOST         Mail server (auto-detected for Gmail, Yahoo, Zoho)
  --allowed-senders LIST   Comma-separated senders allowed to print (default: anyone)
  --poll-seconds N         How often to check the mailbox (default 15)
  --no-replies             Don't email senders whether their file printed
  --smtp-host HOST         Mail server for those replies (default: from --imap-host)
  --status-port N          Port of the status page (default 8631, 0 = off)
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
	case "printers":
		err = cmdPrinters()
	case "test-print":
		err = cmdTestPrint()
	case "pair":
		err = cmdPair(os.Args[2:])
	case "unpair":
		err = cmdUnpair()
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
