# Print Agent: email to printer

Send an email with a PDF attached and it prints on your office printer, **from anywhere**.

```
Anyone, any network ──email + PDF──▶ mailbox (e.g. Gmail)
                                          │  checked every 15 seconds
                         Print Agent (a computer in the office)
                                          ▼
                              Network printer (by IP)
```

Print Agent is a small background program for **Windows, macOS and Linux**. It
watches a mailbox and prints every **PDF, JPG or PNG** attachment that arrives.
It works with ordinary network printers, including printers whose built-in
"HP ePrint" / cloud printing doesn't work.

## Install (one command)

Run it on a computer that **stays on** and is on the **same Wi-Fi/LAN as the printer**.

**Linux / macOS** (Terminal):
```bash
curl -fsSL https://raw.githubusercontent.com/vinayk-kumar/printer/main/install.sh | sh
```

**Windows** (PowerShell):
```powershell
irm https://raw.githubusercontent.com/vinayk-kumar/printer/main/install.ps1 | iex
```

It asks four questions and checks each answer before saving:

```
Printer IP address: 192.168.1.50
Mailbox email: shop.printer@gmail.com
App password (hidden while typing): ****************
Only print emails from (comma-separated, Enter = anyone):

Checking mailbox shop.printer@gmail.com...      ✓ (120 emails already there will not be printed)
Checking printer 192.168.1.50...                ✓
Adding printer to this computer...              ✓ (PrintAgent)
Starting background agent...                    ✓ (starts automatically with this computer)
```

Done. Every new email to that mailbox with a PDF/JPG/PNG attached now prints.
Emails that were already in the mailbox are never printed.

To change any setting, run the same command again (Enter keeps the current value).

### Without questions (for scripts and support teams)

```bash
curl -fsSL https://raw.githubusercontent.com/vinayk-kumar/printer/main/install.sh | \
  sh -s -- --printer-ip 192.168.1.50 --email shop.printer@gmail.com \
           --password "xxxx xxxx xxxx xxxx" --yes
```

| Option | Meaning |
|---|---|
| `--printer-ip` | Printer IP address |
| `--email` | Mailbox that receives print emails |
| `--password` | App password for that mailbox (or set `PRINT_AGENT_PASSWORD`) |
| `--imap-host` | Mail server. Auto-detected for Gmail, Yahoo, Zoho; otherwise `imap.<your-domain>` |
| `--allowed-senders` | Comma-separated senders allowed to print. Default: anyone |
| `--poll-seconds` | How often to check the mailbox. Default: 15 |
| `--yes` | Don't ask anything |

## What you need

| | |
|---|---|
| **A computer that stays on** | In the same network as the printer: an office PC, a Mac, or a Raspberry Pi. It prints only while this computer is on. |
| **A fixed printer IP** | Reserve the printer's IP in your router (DHCP reservation). If the IP changes, run the install command again with the new IP. |
| **A mailbox + app password** | A Gmail account is easiest: turn on 2-Step Verification, then create an app password at <https://myaccount.google.com/apppasswords>. Outlook/Hotmail/Microsoft 365 mailboxes don't accept app passwords for this. |
| **Linux only** | The CUPS printing system (`sudo apt install cups`), and permission to add printers (`sudo usermod -aG lpadmin $USER`). |

We recommend a **separate mailbox only for printing**, because every email with an
attachment that reaches it will print (unless you set allowed senders).

## Everyday commands

The installer prints the program's full path. By default it is
`~/.local/bin/print-agent` (Linux/macOS) or `%LOCALAPPDATA%\PrintAgent\print-agent.exe` (Windows).

| Command | What it does |
|---|---|
| `print-agent status` | Settings, printer state, whether the agent runs, recent activity |
| `print-agent test-print` | Prints a test page |
| `print-agent install` | Change settings |
| `print-agent uninstall` | Stop and remove the agent (`--purge` also deletes settings) |

## Troubleshooting

| Problem | Fix |
|---|---|
| Email sent but nothing prints | Run `print-agent status`. Check that the agent is running, the computer is on the printer's network, and the printer has paper. |
| Email from a new sender doesn't print | Check the mailbox's **Spam** folder. Mail without a subject is often marked as spam. Mark it "Not spam", or add a filter "Has attachment → Never send to Spam". |
| "login failed" | Use an **app password**, not the normal password. For Gmail, 2-Step Verification must be on. |
| "printer not reachable" | The computer must be on the same Wi-Fi/LAN as the printer. Check the IP on the printer's network report. |
| Printer was out of paper | Linux/macOS: the agent resumes the queue by itself after you load paper. Windows: clear the error on the printer. |
| Windows: "could not add the printer automatically" | Add the printer once in *Settings → Bluetooth & devices → Printers & scanners*, then run the install command again. The agent uses the printer you added. |

## How it works

- **Mailbox:** the agent connects to the mailbox over IMAP (TLS) every 15 seconds,
  read-only, so emails stay unread. It remembers the last email it handled, so each
  email prints once, even after a restart.
- **Printer:** on Linux and macOS it adds the printer to CUPS by IP (IPP Everywhere /
  AirPrint), which converts PDFs for the printer. On Windows it uses a Windows printer
  for that IP, and prints PDFs with [SumatraPDF](https://www.sumatrapdfreader.org/)
  (downloaded by the installer).
- **Runs in the background:** as a systemd user service (Linux), a LaunchAgent (macOS),
  or a hidden program in the Startup folder (Windows). It restarts automatically.
- **Settings:** stored only on that computer, readable only by that user:
  `~/.config/print-agent/` (Linux), `~/Library/Application Support/print-agent/`
  (macOS), `%APPDATA%\print-agent\` (Windows). Logs are in `agent.log` in the same folder.

## For developers

```bash
go test ./...                 # tests
go build -o print-agent .     # build for this computer
```

Release a new version: `git tag v0.1.1 && git push origin v0.1.1`. GitHub Actions
tests, builds Windows/macOS/Linux binaries and attaches them to the release. The
install commands always download the latest release.
