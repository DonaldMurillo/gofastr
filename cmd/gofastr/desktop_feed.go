package main

// The `gofastr desktop feed` verb: build the signed update feed for one
// platform archive. The engine is battery/desktop's SignUpdateFeed, so
// the writer and the updater's verifier share one implementation of
// the format (semver, the https rule, the single-.app zip check, the
// sha256/size computation, and the ed25519 signature over the exact
// manifest bytes).

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
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
		name, value, hasValue := splitFlagEqual(args[i])
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
			osExit(0)
		default:
			fail("Unknown feed flag: %s", args[i])
			osExit(1)
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
		fail("feed is missing %s", strings.Join(missing, " "))
		printDesktopUsage()
		osExit(1)
		return
	}
	if err := desktop.SignUpdateFeed(f.out, f.key, f.version, f.notes, f.platform, f.archive, f.url); err != nil {
		fail("feed: %v", err)
		osExit(1)
		return
	}
	success("manifest.json and manifest.json.sig written to %s", f.out)
	info("publish both files at the feed URL the app UpdateConfig points at")
}
