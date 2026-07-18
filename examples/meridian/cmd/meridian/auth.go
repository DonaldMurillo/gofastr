package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// parseOrHelp parses args, mapping --help to exit 0 and bad flags to 2.
func parseOrHelp(fs *flag.FlagSet, args []string) (ok bool, code int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	return true, 0
}

// runLogin stores the server URL and an API token. Tokens are minted in the
// app (a logged-in browser session POSTs /auth/tokens); the CLI only stores
// one. --with-token reads it from stdin so it never appears in shell history
// or on screen:
//
//	echo "$TOKEN" | meridian login --url https://app.example.com --with-token
func runLogin(args []string) int {
	fs := newFlagSet("login")
	urlF := fs.String("url", "", "server URL to store (e.g. https://app.example.com)")
	withToken := fs.Bool("with-token", false, "read the API token from stdin")
	if ok, code := parseOrHelp(fs, args); !ok {
		return code
	}
	cfg := loadConfig()
	if *urlF != "" {
		cfg.URL = strings.TrimRight(strings.TrimSpace(*urlF), "/")
	}
	if cfg.URL == "" {
		fmt.Fprintf(os.Stderr, "no server URL: pass --url https://your-app.example.com\n")
		return 2
	}
	var token string
	if *withToken {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		token = strings.TrimSpace(string(data))
	} else {
		// No termios in the stdlib, so interactive input echoes; warn and
		// point at the pipe-friendly path.
		fmt.Fprintf(os.Stderr, "Paste an API token minted in the app (input will echo — prefer --with-token via a pipe): ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		token = strings.TrimSpace(line)
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "no token provided")
		return 2
	}
	cfg.Token = token
	path, err := saveConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Logged in to %s — config stored at %s\n", cfg.URL, path)
	return 0
}

// runLogout removes the stored token (the URL is kept for the next login).
func runLogout(args []string) int {
	fs := newFlagSet("logout")
	if ok, code := parseOrHelp(fs, args); !ok {
		return code
	}
	cfg := loadConfig()
	if cfg.Token == "" {
		fmt.Println("not logged in")
		return 0
	}
	cfg.Token = ""
	if _, err := saveConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Logged out — stored token removed. Revoke it in the app to invalidate it server-side.")
	return 0
}
