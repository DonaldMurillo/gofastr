package desktopcmd

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/devcmd"
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
	for i := 0; i < len(args); i++ {
		name, value, hasValue := devcmd.SplitFlagEqual(args[i])
		if !hasValue && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			value = args[i+1]
			hasValue = true
			i++
		}
		switch name {
		case "--key":
			f.key = value
		case "--version":
			f.version = value
		case "--notes":
			f.notes = value
		case "--platform":
			f.platform = value
		case "--archive":
			f.archive = value
		case "--url":
			f.url = value
		case "-o", "--o", "-out", "--out":
			f.out = value
		case "--help", "-h":
			printDesktopUsage()
			cli.OsExit(0)
		default:
			cli.Fail("Unknown feed flag: %s", args[i])
			cli.OsExit(1)
		}
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
