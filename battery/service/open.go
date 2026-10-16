package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// loginPageMaxAge is how long a spent sign-in page may sit in the
// login directory before the next `open` deletes it. Its token died
// after 60 seconds; the file is only clutter by then.
const loginPageMaxAge = 5 * time.Minute

// openWith asks the daemon of layout l for a sign-in token and opens
// the browser on it. The token travels inside a 0600 page that POSTs
// it, so it never appears in argv (visible to every local user in
// `ps`), the browser's history, or a Referer. With print, it prints a
// one-shot URL instead, which Config.InsecureTokenInURL must allow.
func (b *Battery) openWith(l layout, print bool) error {
	if print && !b.cfg.InsecureTokenInURL {
		return errors.New("service: --print needs Config.InsecureTokenInURL: a token in a URL leaks through argv and browser history")
	}
	resp, err := callControl(l.socketPath(), "token")
	if errors.Is(err, errNotRunning) {
		return fmt.Errorf("service: %s is not running; start it with `%s service start` (or `%s service run` for a dev run)", b.cfg.Name, b.cfg.Name, b.cfg.Name)
	}
	if err != nil {
		return err
	}
	loginURL := "http://" + resp.Addr + loginPath
	if print {
		fmt.Fprintf(b.cfg.Stdout, "%s?t=%s\n(single use, expires in %s)\n", loginURL, resp.Token, loginTokenTTL)
		return nil
	}
	if err := ensurePrivateDir(l.loginDir()); err != nil {
		return err
	}
	removeStalePages(l.loginDir())
	f, err := os.CreateTemp(l.loginDir(), "open-*.html")
	if err != nil {
		return fmt.Errorf("service: write sign-in page: %w", err)
	}
	if _, err := f.WriteString(signInPage(loginURL, resp.Token)); err != nil {
		f.Close()
		return fmt.Errorf("service: write sign-in page: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("service: write sign-in page: %w", err)
	}
	if err := b.launchBrowser(f.Name()); err != nil {
		fmt.Fprintf(b.cfg.Stderr, "could not open a browser (%v)\nopen this file within a minute: %s\n", err, f.Name())
		return nil
	}
	return nil
}

// removeStalePages deletes sign-in pages older than loginPageMaxAge.
func removeStalePages(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-loginPageMaxAge)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// launchBrowser opens path in the user's default browser. On macOS a
// bare `open file.html` follows the .html file association, which on
// many machines is an editor; `open -b` names the browser instead.
func (b *Battery) launchBrowser(path string) error {
	switch currentOS {
	case "darwin":
		if id := b.defaultBrowser(); id != "" {
			return b.run("open", "-b", id, path)
		}
		return b.run("open", path)
	case "windows":
		return b.run("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		return b.run("xdg-open", path)
	}
}

// launchServicesPrefs is the per-user LaunchServices store, relative to
// the home directory: the handler the user picked for each URL scheme.
const launchServicesPrefs = "Library/Preferences/com.apple.LaunchServices/com.apple.launchservices.secure.plist"

// defaultBrowser is the bundle id of the macOS handler for http URLs,
// read from the LaunchServices store with plutil. No store, or no http
// entry in it, means the system default, Safari. "" (fall back to a
// bare open) when the store cannot be read or names something that is
// not a bundle id.
func (b *Battery) defaultBrowser() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, launchServicesPrefs)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return "com.apple.Safari"
	}
	out, err := b.tool.output("plutil", "-extract", "LSHandlers", "json", "-o", "-", path)
	if err != nil {
		return ""
	}
	var handlers []struct {
		Scheme string `json:"LSHandlerURLScheme"`
		Role   string `json:"LSHandlerRoleAll"`
	}
	if err := json.Unmarshal(out, &handlers); err != nil {
		return ""
	}
	for _, h := range handlers {
		if strings.EqualFold(h.Scheme, "http") {
			if !bundleID.MatchString(h.Role) {
				return ""
			}
			return h.Role
		}
	}
	return "com.apple.Safari"
}

// bundleID is a reverse-DNS bundle identifier; anything else is not
// passed to `open -b`.
var bundleID = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
