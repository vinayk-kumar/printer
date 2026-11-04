# Print Agent: email to printer

Send an email with a PDF attached and it prints on your office printer, **from anywhere**.

```
Anyone, any network ──email + PDF──▶ mailbox (e.g. Gmail)
                                          │  checked every 15 seconds
                  Print Agent (office PC, Mac or Raspberry Pi)
                                          ▼
                     Printer: USB cable, or network (by IP)
```

Print Agent is a small background program for **Windows, macOS, Linux and
Raspberry Pi**. It watches a mailbox and prints every **PDF, JPG or PNG**
attachment that arrives. It works with ordinary printers: **USB printers**
(e.g. HP LaserJet M1005) and **network printers**, including printers whose
built-in "HP ePrint" / cloud printing doesn't work.

## Install (one command)

Run it on a computer that **stays on**, and is **connected to the printer by
USB** or on the **same Wi-Fi/LAN as a network printer**.

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
Printers on this computer:
  1) HP LaserJet M1005                  USB
  2) HP_M126nw                          network 192.168.4.84

Printer: choose a number, or type a network printer's IP: 1
Mailbox email: shop.printer@gmail.com
App password (hidden while typing): ****************
Only print emails from (comma-separated, Enter = anyone):

Checking mailbox shop.printer@gmail.com...      ✓ (120 emails already there will not be printed)
Checking printer HP LaserJet M1005...           ✓ (USB)
Starting background agent...                    ✓ (starts automatically with this computer)
```

For a network printer that isn't in the list, type its IP address instead of a
number; the agent adds it to the computer for you.

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
| `--printer` | A printer installed on this computer (USB or network), by name as shown by `print-agent printers` |
| `--printer-ip` | Or a network printer's IP address |
| `--email` | Mailbox that receives print emails |
| `--password` | App password for that mailbox (or set `PRINT_AGENT_PASSWORD`) |
| `--imap-host` | Mail server. Auto-detected for Gmail, Yahoo, Zoho; otherwise `imap.<your-domain>` |
| `--allowed-senders` | Comma-separated senders allowed to print. Default: anyone |
| `--poll-seconds` | How often to check the mailbox. Default: 15 |
| `--yes` | Don't ask anything |

## Did it print? Status replies and the status page

**Reply to the sender.** After each email, Print Agent replies to the sender from
the printer's mailbox (same app password), in the same email thread:

```
✓ Printed

  ✓ invoice.pdf: printed
  – data.zip: skipped (only PDF, JPG and PNG files are printed)

Printer: HP LaserJet M1005
Received: 4 Nov 2026, 16:20
```

If the file is still in the printer queue after 2 minutes (out of paper, paper jam,
printer off...), the reply says **Not printed yet** with the reason, and it prints
as soon as the problem is fixed. Emails with no PDF/JPG/PNG get a "Nothing to print"
reply. Newsletters, no-reply addresses and other automatic mail never get a reply,
and senders that aren't allowed are ignored silently. Turn replies off with
`--no-replies`.

**Status page (phone app).** The installer and `print-agent status` show a link like
`http://192.168.1.20:8631/?key=…`. Open it on a phone on the same Wi-Fi (or on the
computer itself) to see:

- the printer and whether it is ready or needs attention (out of paper, cover open...);
- whether the agent is running and when it last checked the mailbox;
- every email: printed, waiting (with the reason), failed or not printed, and per file;
- a **Print a test page** button.

On the phone, use the browser's **Add to Home screen** to keep it as an app. Keep the
link private: anyone with it on your network can see sender addresses and subjects.
Windows and macOS may ask once to allow Print Agent on the network; allow it for
private networks. Use `--status-port N` for another port, or `--status-port 0` to
turn the page off.

## Check your printers from anywhere: the cloud dashboard

The status page above works only on the office network. To check from anywhere, and
see several offices in one place, run the **cloud dashboard**, `print-cloud`, on a
small VM. Each office computer reports to it; you log in from any phone or browser.

```
Office PC (Print Agent) ──HTTPS, every 15 s──▶ print-cloud on a VM ◀── your phone, anywhere
   printer state, emails and results               (login, all printers)
   ◀── "print a test page"
```

The dashboard shows each printer: online or offline, ready or needing attention (out
of paper, offline...), every email with its result, and a test-print button. It
installs on the phone's home screen like an app.

### Set up the VM (once)

Any small Linux VM with a public IP works (1 vCPU and 512 MB RAM is plenty), plus a
domain name pointing at it (e.g. `print.yourshop.com`: a DNS A record to the VM's IP).
Ports 80 and 443 must be open.

```bash
# 1. The server
curl -fsSL -o print-cloud https://github.com/vinayk-kumar/printer/releases/latest/download/print-cloud-linux-amd64
sudo install print-cloud /usr/local/bin/
sudo useradd --system --home-dir /var/lib/print-cloud --shell /usr/sbin/nologin print-cloud
curl -fsSL -o print-cloud.service https://raw.githubusercontent.com/vinayk-kumar/printer/main/deploy/print-cloud.service
sudo cp print-cloud.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now print-cloud

# 2. HTTPS with Caddy, which gets the certificate by itself
sudo apt install -y caddy
printf 'print.yourshop.com {\n\treverse_proxy 127.0.0.1:8080\n}\n' | sudo tee /etc/caddy/Caddyfile
sudo systemctl reload caddy

# 3. A login for you (run again to change the password)
sudo -u print-cloud print-cloud set-user you@yourshop.com --db /var/lib/print-cloud/print-cloud.db
```

On an ARM VM, download `print-cloud-linux-arm64` instead.

### Add an office printer

1. Open `https://print.yourshop.com`, log in and tap **Add a printer**. It shows a
   6-digit code and the command to run.
2. On the office computer (Print Agent already installed), run that command:
   ```bash
   print-agent pair https://print.yourshop.com 123456
   ```
3. The printer appears on the dashboard within 15 seconds.

`print-agent unpair` stops reporting; **Remove from dashboard** does the same from the
web. The office computer only makes outgoing HTTPS connections, so nothing has to be
opened on the office router. Sender addresses and subjects are stored on your VM.

## What you need

| | |
|---|---|
| **A computer that stays on** | An office PC, a Mac, or a Raspberry Pi, with internet. It prints only while this computer is on. No PC that runs all day? Use a Raspberry Pi (see below). |
| **The printer connected to it** | **USB printer:** plug it into that computer and install it there first (see below). **Network printer:** the computer must be on the same Wi-Fi/LAN. |
| **A mailbox + app password** | A Gmail account is easiest: turn on 2-Step Verification, then create an app password at <https://myaccount.google.com/apppasswords>. Outlook/Hotmail/Microsoft 365 mailboxes don't accept app passwords for this. |
| **Linux / Raspberry Pi** | The installer sets up the CUPS printing system itself (Debian, Ubuntu, Raspberry Pi OS). |

We recommend a **separate mailbox only for printing**, because every email with an
attachment that reaches it will print (unless you set allowed senders).

## Connecting the printer

**Any brand works**: HP, Canon, Epson, Brother, Samsung, Ricoh and others. Print Agent
prints through the computer's own printing system, so any printer that computer can
print to (USB or network) works. The only brand-specific part is a convenience on
Linux: for an HP USB printer the installer also installs HP's driver for you.

### USB printers (e.g. HP LaserJet M1005)

USB printers have **no IP address**. Plug the printer into the computer that runs
Print Agent, make sure the computer can print to it, then run the install command
and choose it by number.

| Computer | Setting up the USB printer |
|---|---|
| **Windows** | Plug it in. Windows usually installs the driver by itself. If not, get the driver from [HP support](https://support.hp.com) (search your model, e.g. "LaserJet M1005"). Print a test page from *Settings → Printers & scanners*. |
| **macOS** | Plug it in, then *System Settings → Printers & Scanners → Add Printer*. |
| **Linux / Raspberry Pi** | Plug it in and run the install command. If it finds an HP USB printer that isn't set up, it installs HP's driver (HPLIP) and starts HP's setup (`hp-setup`). Older HP LaserJets such as the M1005 need HP's plugin: accept the download when HP's setup asks. Other brands: set the printer up in CUPS first (`http://localhost:631`). |

### Network printers

Type the printer's IP address when asked. To find it:

- Print the printer's **network configuration / network summary report** from its
  control panel (on many HP printers: hold the Wireless or Go/Cancel button for a
  few seconds).
- Or look for the printer in your **router's list of connected devices**.
- Then **reserve that IP in the router** (DHCP reservation) so it never changes.
  If it changes, run the install command again with the new IP.

### No computer running all day? Use a Raspberry Pi

A Raspberry Pi is a small, low-power computer (about 5 W) that can run Print Agent
24×7 next to the printer:

1. Install **Raspberry Pi OS (64-bit)** on a microSD card with Raspberry Pi Imager
   (set Wi-Fi and enable SSH there, or plug in a LAN cable).
2. Connect the printer's USB cable to the Pi (or put the Pi on the same network as a
   network printer).
3. Open a terminal on the Pi (or SSH into it) and run the Linux install command above.

Any Raspberry Pi that runs the 64-bit OS works (Pi 3, 4, 5, Zero 2 W; the Zero 2 W
needs a micro-USB adapter for the printer cable).

## Everyday commands

The installer prints the program's full path. By default it is
`~/.local/bin/print-agent` (Linux/macOS) or `%LOCALAPPDATA%\PrintAgent\print-agent.exe` (Windows).

| Command | What it does |
|---|---|
| `print-agent status` | Settings, printer state, whether the agent runs, recent activity |
| `print-agent printers` | Printers installed on this computer |
| `print-agent test-print` | Prints a test page |
| `print-agent install` | Change settings |
| `print-agent pair URL CODE` | Show this printer on your cloud dashboard |
| `print-agent unpair` | Stop reporting to the cloud dashboard |
| `print-agent uninstall` | Stop and remove the agent (`--purge` also deletes settings) |

## Troubleshooting

| Problem | Fix |
|---|---|
| Email sent but nothing prints | Run `print-agent status`. Check that the agent is running, the computer is on the printer's network, and the printer has paper. |
| Email from a new sender doesn't print | Check the mailbox's **Spam** folder. Mail without a subject is often marked as spam. Mark it "Not spam", or add a filter "Has attachment → Never send to Spam". |
| "login failed" | Use an **app password**, not the normal password. For Gmail, 2-Step Verification must be on. |
| "printer not reachable" | The computer must be on the same Wi-Fi/LAN as the printer. Check the IP on the printer's network report. |
| USB printer not in the list | Install it in the system first (see *USB printers*), print a test page from the system, then run the install command again. |
| Printer was out of paper | Linux/macOS: the agent resumes the queue by itself after you load paper. Windows: clear the error on the printer. |
| Windows: "could not add the printer automatically" | Add the printer once in *Settings → Bluetooth & devices → Printers & scanners*, then run the install command again. The agent uses the printer you added. |

## How it works

- **Mailbox:** the agent connects to the mailbox over IMAP (TLS) every 15 seconds,
  read-only, so emails stay unread. It remembers the last email it handled, so each
  email prints once, even after a restart.
- **Printer:** it prints to a printer installed on the computer, using its driver,
  so USB printers work. If you give an IP instead, it adds that network printer
  first (CUPS IPP Everywhere / AirPrint on Linux and macOS; IPP on Windows 11).
  On Windows, PDFs are printed with [SumatraPDF](https://www.sumatrapdfreader.org/)
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
go build ./cmd/print-cloud    # the cloud dashboard server

# Try the dashboard locally (pairing accepts http:// for localhost only):
PRINT_CLOUD_PASSWORD=test-pass ./print-cloud set-user me@test.com
./print-cloud serve           # http://127.0.0.1:8080
```

Release a new version: `git tag v0.1.1 && git push origin v0.1.1`. GitHub Actions
tests, builds the Windows/macOS/Linux agents and the Linux `print-cloud` server, and
attaches them to the release. The
install commands always download the latest release.
