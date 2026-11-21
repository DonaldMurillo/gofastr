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

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const loginPageMaxAge = 5 * time.Minute

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
	if err := fileperm.RestrictOpenFile(f); err != nil {
		f.Close()
		return fmt.Errorf("service: restrict sign-in page: %w", err)
	}
	if _, err := f.WriteString(signInPage(openCtx(), loginURL, resp.Token)); err != nil {
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

const launchServicesPrefs = "Library/Preferences/com.apple.LaunchServices/com.apple.launchservices.secure.plist"

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

var bundleID = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
