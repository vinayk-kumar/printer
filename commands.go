package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"
)

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	email := fs.String("email", "", "")
	password := fs.String("password", "", "")
	printerIP := fs.String("printer-ip", "", "")
	printerName := fs.String("printer", "", "")
	imapHost := fs.String("imap-host", "", "")
	allowed := fs.String("allowed-senders", "", "")
	poll := fs.Int("poll-seconds", 0, "")
	smtpHostFlag := fs.String("smtp-host", "", "")
	noReplies := fs.Bool("no-replies", false, "")
	statusPort := fs.Int("status-port", 0, "")
	yes := fs.Bool("yes", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if *password == "" {
		*password = os.Getenv("PRINT_AGENT_PASSWORD")
	}

	old, _ := loadConfig()
	if old == nil {
		old = &Config{}
	}
	in := newPrompter(!*yes)
	fmt.Println("Print Agent setup: emails sent to the mailbox below will print automatically.")
	fmt.Println("Press Enter to keep the value shown in [brackets].")
	fmt.Println()

	printers, err := listPrinters()
	if err != nil {
		return err
	}
	c := &Config{}
	if c.PrinterIP, c.PrinterName, err = choosePrinter(in, *printerIP, *printerName, old, printers); err != nil {
		return err
	}
	c.Email = in.ask("Mailbox email", *email, old.Email)
	sameMailbox := strings.EqualFold(c.Email, old.Email)
	c.Password = in.askSecret("App password", *password, ifThen(sameMailbox, old.Password))
	c.IMAPHost = firstOf(*imapHost, ifThen(sameMailbox, old.IMAPHost), guessIMAPHost(c.Email))
	senders := strings.Join(old.AllowedSenders, ", ")
	if given["allowed-senders"] {
		senders = *allowed
	} else {
		senders = in.ask("Only print emails from (comma-separated, Enter = anyone)", "", senders)
	}
	c.AllowedSenders = splitList(senders)
	c.PollSeconds = 15
	if *poll >= 5 {
		c.PollSeconds = *poll
	} else if old.PollSeconds >= 5 {
		c.PollSeconds = old.PollSeconds
	}
	c.SMTPHost = firstOf(*smtpHostFlag, ifThen(sameMailbox, old.SMTPHost))
	c.NoReplies = old.NoReplies
	if given["no-replies"] {
		c.NoReplies = *noReplies
	}
	c.StatusPort = old.StatusPort
	if given["status-port"] {
		c.StatusPort = *statusPort
		if *statusPort == 0 {
			c.StatusPort = -1 // off
		}
	}
	c.StatusKey = firstOf(old.StatusKey, newStatusKey())
	if (c.PrinterIP == "" && c.PrinterName == "") || c.Email == "" || !strings.Contains(c.Email, "@") || c.Password == "" {
		return errors.New("a printer, mailbox email and app password are required (run in a terminal, or pass --printer or --printer-ip, --email and --password)")
	}
	fmt.Println()

	step("Checking mailbox " + c.Email)
	n, err := checkMailbox(c)
	if err != nil {
		fmt.Println("✗")
		return fmt.Errorf("%v\n  %s", err, loginHint(c))
	}
	fmt.Printf("✓ (%d emails already there will not be printed)\n", n)

	if c.PrinterIP != "" {
		step("Checking printer " + c.PrinterIP)
		if err := printerReachable(c.PrinterIP); err != nil {
			fmt.Println("✗")
			return fmt.Errorf("printer not reachable: %v\n  This computer must be on the same network (Wi-Fi/LAN) as the printer", err)
		}
		fmt.Println("✓")

		step("Adding printer to this computer")
		if c.PrinterName, err = setupPrinter(c.PrinterIP); err != nil {
			fmt.Println("✗")
			return err
		}
		fmt.Printf("✓ (%s)\n", c.PrinterName)
	} else {
		step("Checking printer " + c.PrinterName)
		p, ok := findPrinter(printers, c.PrinterName)
		if !ok {
			fmt.Println("✗")
			return fmt.Errorf("no printer called %q on this computer. See the list with: print-agent printers", c.PrinterName)
		}
		c.PrinterName = p.Name
		fmt.Printf("✓ (%s)\n", p.Connection)
	}

	step("Starting background agent")
	_ = stopService()
	exe, err := installBinary()
	if err != nil {
		fmt.Println("✗")
		return err
	}
	if err := saveConfig(c); err != nil {
		fmt.Println("✗")
		return err
	}
	if err := installService(exe); err != nil {
		fmt.Println("✗")
		return err
	}
	fmt.Println("✓ (starts automatically with this computer)")

	fmt.Printf(`
Done! Emails with a PDF, JPG or PNG attached sent to %s
will now print on %s.
`, c.Email, printerLabel(c))
	if !c.NoReplies {
		fmt.Println("Each sender gets an email reply saying whether their file printed.")
	}
	if u := statusURL(c); u != "" {
		fmt.Printf("\nStatus page (open on a phone on the same Wi-Fi, or on this computer):\n  %s\n", u)
	}
	fmt.Printf(`
  Check status : %s status
  Test page    : %s test-print
  Change setup : run the install command again
  Remove       : %s uninstall
`, exe, exe, exe)
	return nil
}

// choosePrinter asks for an installed printer (by number) or a network
// printer's IP. Returns the IP to add, or the installed printer's name.
func choosePrinter(in *prompter, ipFlag, nameFlag string, old *Config, printers []printerInfo) (string, string, error) {
	if ipFlag != "" {
		return strings.TrimSpace(ipFlag), "", nil
	}
	if nameFlag != "" {
		return "", strings.TrimSpace(nameFlag), nil
	}
	if !in.interactive {
		return old.PrinterIP, ifThen(old.PrinterIP == "", old.PrinterName), nil
	}
	if len(printers) > 0 {
		fmt.Println("Printers on this computer:")
		for i, p := range printers {
			fmt.Printf("  %d) %-34s %s\n", i+1, p.Name, p.Connection)
		}
	} else {
		fmt.Println("No printers are installed on this computer yet.")
		fmt.Println("USB printer? Connect it, install it in the system's printer settings, then run this again.")
	}
	fmt.Println()
	current := firstOf(old.PrinterIP, old.PrinterName)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		answer := in.ask("Printer: choose a number, or type a network printer's IP", "", current)
		if answer == old.PrinterIP && answer != "" {
			return old.PrinterIP, "", nil
		}
		var ip, name string
		if ip, name, err = parsePrinterChoice(answer, printers); err == nil {
			return ip, name, nil
		}
		fmt.Println("  ✗", err)
	}
	return "", "", err
}

func printerLabel(c *Config) string {
	if c.PrinterIP != "" {
		return "the printer at " + c.PrinterIP
	}
	return "printer " + c.PrinterName
}

func cmdPrinters() error {
	printers, err := listPrinters()
	if err != nil {
		return err
	}
	if len(printers) == 0 {
		fmt.Println("No printers are installed on this computer.")
		return nil
	}
	for i, p := range printers {
		fmt.Printf("%d) %-34s %s\n", i+1, p.Name, p.Connection)
	}
	return nil
}

func cmdStatus() error {
	c, err := loadConfig()
	if err != nil {
		return errors.New("not set up yet. Run: print-agent install")
	}
	senders := "anyone"
	if len(c.AllowedSenders) > 0 {
		senders = strings.Join(c.AllowedSenders, ", ")
	}
	fmt.Printf("Mailbox    : %s (%s)\n", c.Email, c.IMAPHost)
	printer := c.PrinterName
	if c.PrinterIP != "" {
		printer = c.PrinterIP + " (" + c.PrinterName + ")"
	}
	fmt.Printf("Printer    : %s - %s\n", printer, printerStatus(c))
	fmt.Printf("Senders    : %s\n", senders)
	fmt.Printf("Agent      : %s\n", serviceStatus())
	replies := "off"
	if !c.NoReplies {
		replies = "on (sent through " + smtpHost(c) + ")"
	}
	fmt.Printf("Replies    : %s\n", replies)
	if u := statusURL(c); u != "" {
		fmt.Printf("Status page: %s\n", u)
	}
	fmt.Printf("Settings   : %s\n", configPath())
	fmt.Printf("Log        : %s\n", logPath())
	if history := loadHistory(); len(history) > 0 {
		fmt.Println("\nRecent emails:")
		for i, r := range history {
			if i == 5 {
				break
			}
			fmt.Printf("  %s  %-8s  %s  %q\n", r.At.Format("02 Jan 15:04"), r.Status, firstOf(r.From, "status page"), r.Subject)
		}
	}
	fmt.Println("\nRecent activity:")
	for _, line := range tail(logPath(), 10) {
		fmt.Println("  " + line)
	}
	return nil
}

func cmdTestPrint() error {
	c, err := loadConfig()
	if err != nil {
		return errors.New("not set up yet. Run: print-agent install")
	}
	if _, err := sendTestPage(c); err != nil {
		return err
	}
	fmt.Println("✓ Test page sent to the printer")
	return nil
}

// sendTestPage prints a test page and returns its print job.
func sendTestPage(c *Config) (string, error) {
	tmp, err := os.CreateTemp("", "print-agent-test-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	tmp.Write(testPagePDF([]string{
		"Print Agent test page",
		"Mailbox: " + c.Email,
		"Printer: " + firstOf(c.PrinterIP, c.PrinterName),
		"Time: " + time.Now().Format("2006-01-02 15:04:05"),
	}))
	tmp.Close()
	return printFile(c, tmp.Name(), "Print Agent test page")
}

func cmdUninstall(args []string) error {
	purge := len(args) > 0 && args[0] == "--purge"
	_ = stopService()
	if err := uninstallService(); err != nil {
		return err
	}
	fmt.Println("✓ Background agent stopped and removed")
	if purge {
		if c, err := loadConfig(); err == nil {
			removePrinter(c)
		}
		if err := os.RemoveAll(configDir()); err != nil {
			return err
		}
		fmt.Println("✓ Settings deleted")
	}
	if runtime.GOOS == "windows" {
		fmt.Println("You can now delete the folder", filepath.Dir(installPath()))
	} else {
		_ = os.Remove(installPath())
	}
	return nil
}

func installPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "PrintAgent", "print-agent.exe")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "print-agent")
}

// installBinary copies the running executable to its permanent location.
func installBinary() (string, error) {
	dst := installPath()
	src, err := os.Executable()
	if err != nil {
		return "", err
	}
	if s, err := filepath.EvalSymlinks(src); err == nil {
		src = s
	}
	if src == dst {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	out.Close()
	return dst, os.Rename(tmp, dst)
}

func printerReachable(ip string) error {
	var lastErr error
	for _, port := range []string{"631", "9100", "80"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), 3*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func loginHint(c *Config) string {
	switch {
	case strings.Contains(c.IMAPHost, "gmail"):
		return "Use a Google App Password, not your normal password (2-Step Verification must be on): https://myaccount.google.com/apppasswords"
	case strings.Contains(c.IMAPHost, "office365"), strings.Contains(c.IMAPHost, "outlook"):
		return "Outlook/Hotmail/Microsoft 365 mailboxes don't accept app passwords for this. Use a Gmail mailbox for the printer."
	default:
		return "Check the email, app password and mail server (set it with --imap-host if it isn't " + c.IMAPHost + ")."
	}
}

type prompter struct {
	interactive bool
	r           *bufio.Reader
}

func newPrompter(interactive bool) *prompter {
	return &prompter{interactive: interactive, r: bufio.NewReader(os.Stdin)}
}

func (p *prompter) ask(label, given, current string) string {
	if given != "" || !p.interactive {
		return strings.TrimSpace(firstOf(given, current))
	}
	if current != "" {
		fmt.Printf("%s [%s]: ", label, current)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := p.r.ReadString('\n')
	return strings.TrimSpace(firstOf(strings.TrimSpace(line), current))
}

func (p *prompter) askSecret(label, given, current string) string {
	if given != "" || !p.interactive {
		return firstOf(given, current)
	}
	if current != "" {
		fmt.Printf("%s [saved, Enter to keep]: ", label)
	} else {
		fmt.Printf("%s (hidden while typing): ", label)
	}
	var line string
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		b, _ := term.ReadPassword(fd)
		fmt.Println()
		line = string(b)
	} else {
		line, _ = p.r.ReadString('\n')
	}
	return firstOf(strings.TrimSpace(line), current)
}

func step(msg string) { fmt.Printf("%-48s ", msg+"...") }

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func ifThen(cond bool, v string) string {
	if cond {
		return v
	}
	return ""
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func tail(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"(no activity yet)"}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
