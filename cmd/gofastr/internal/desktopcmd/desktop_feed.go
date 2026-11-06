package desktopcmd

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// desktopFeedFlags carries `desktop feed` flags.
type desktopFeedFlags struct {
	key      string
	version  string
	notes    string
	platform string
	archive  string
	url      string
	out      string
}

// parseDesktopFeedFlags parses the feed flag list. Nothing has a
// default: every value is a release decision.
func parseDesktopFeedFlags(args []string) desktopFeedFlags {
	var f desktopFeedFlags
	var help bool
	if _, err := cli.ParseFlags(args, map[string]*string{
		"--key": &f.key, "--version": &f.version, "--notes": &f.notes,
		"--platform": &f.platform, "--archive": &f.archive, "--url": &f.url,
		"-o": &f.out, "--o": &f.out, "-out": &f.out, "--out": &f.out,
	}, map[string]*bool{"--help": &help, "-h": &help}); err != nil {
		cli.Fail("feed: %v", err)
		cli.OsExit(1)
		return f
	}
	if help {
		printDesktopUsage()
		cli.OsExit(0)
	}
	if f.out == "" {
		f.out = "."
	}
	return f
}

// runDesktopFeed writes manifest.json and manifest.json.sig into -o
// for one platform archive, refusing anything SignUpdateFeed refuses.
func runDesktopFeed(args []string) {
	f := parseDesktopFeedFlags(args)
	missing := make([]string, 0, 5)
	if f.key == "" {
		missing = append(missing, "--key")
	}
	if f.version == "" {
		missing = append(missing, "--version")
	}
	if f.platform == "" {
		missing = append(missing, "--platform")
	}
	if f.archive == "" {
		missing = append(missing, "--archive")
	}
	if f.url == "" {
		missing = append(missing, "--url")
	}
	if len(missing) > 0 {
		cli.Fail("feed is missing %s", strings.Join(missing, " "))
		printDesktopUsage()
		cli.OsExit(1)
		return
	}
	if err := desktop.SignUpdateFeed(f.out, f.key, f.version, f.notes, f.platform, f.archive, f.url); err != nil {
		cli.Fail("feed: %v", err)
		cli.OsExit(1)
		return
	}
	cli.Success("manifest.json and manifest.json.sig written to %s", f.out)
	cli.Info("publish both files at the feed URL the app UpdateConfig points at")
}
