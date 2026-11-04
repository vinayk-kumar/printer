// print-cloud is the Print Agent cloud dashboard server. Agents on office PCs
// pair with it and report their printer and print history; owners log in from
// anywhere to check it. Run it on a small VM behind Caddy (HTTPS).
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vinayk-kumar/printer/cloud"
	"golang.org/x/term"
)

var version = "dev"

const usage = `print-cloud %s - Print Agent cloud dashboard.

Usage:
  print-cloud serve [--addr 127.0.0.1:8080] [--db print-cloud.db]
      Run the server. Put Caddy (or another HTTPS proxy) in front of it.
  print-cloud set-user EMAIL [--db print-cloud.db]
      Add a user who can log in, or change their password
      (asks for it, or set PRINT_CLOUD_PASSWORD).
  print-cloud version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Printf(usage, version)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "set-user":
		err = setUser(os.Args[2:])
	case "version", "--version":
		fmt.Println(version)
	default:
		fmt.Printf(usage, version)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on")
	db := fs.String("db", "print-cloud.db", "SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := cloud.OpenStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           cloud.NewServer(store).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("print-cloud %s listening on %s (database %s)", version, *addr, *db)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func setUser(args []string) error {
	fs := flag.NewFlagSet("set-user", flag.ContinueOnError)
	db := fs.String("db", "print-cloud.db", "SQLite database file")
	// Allow the email before or after the flags.
	var email string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		email, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if email == "" {
		email = fs.Arg(0)
	}
	if email == "" {
		return errors.New("usage: print-cloud set-user EMAIL [--db FILE]")
	}
	password := os.Getenv("PRINT_CLOUD_PASSWORD")
	if password == "" {
		var err error
		if password, err = askPassword("Password (8+ characters): "); err != nil {
			return err
		}
		again, err := askPassword("Same password again: ")
		if err != nil {
			return err
		}
		if again != password {
			return errors.New("the passwords are different")
		}
	}
	store, err := cloud.OpenStore(*db)
	if err != nil {
		return err
	}
	defer store.Close()
	added, err := store.SetUser(email, password)
	if err != nil {
		return err
	}
	if added {
		fmt.Println("✓ User added:", email)
	} else {
		fmt.Println("✓ Password changed:", email)
	}
	return nil
}

func askPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}
