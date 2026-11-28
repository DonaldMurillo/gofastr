package desktopcmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

type desktopFeedFlags struct {
	key       string
	version   string
	notes     string
	platform  string
	archive   string
	url       string
	out       string
	validDays string
}

func parseDesktopFeedFlags(args []string) desktopFeedFlags {
	var f desktopFeedFlags
	var help bool
	if _, err := cli.ParseFlags(args, map[string]*string{
		"--key": &f.key, "--version": &f.version, "--notes": &f.notes,
		"--platform": &f.platform, "--archive": &f.archive, "--url": &f.url,
		"--valid-days": &f.validDays,
		"-o":           &f.out, "--o": &f.out, "-out": &f.out, "--out": &f.out,
	}, map[string]*bool{"--help": &help, "-h": &help}); err != nil {
		cli.UsageFail("feed: %v", err)
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
		cli.OsExit(2)
		return
	}
	validFor, err := feedLifetime(f.validDays)
	if err != nil {
		cli.Fail("feed: %v", err)
		cli.OsExit(1)
		return
	}
	if err := desktop.SignUpdateFeed(f.out, f.key, f.version, f.notes, f.platform, f.archive, f.url, validFor); err != nil {
		cli.Fail("feed: %v", err)
		cli.OsExit(1)
		return
	}
	if validFor == 0 {
		validFor = desktop.DefaultFeedLifetime
	}
	days := int(validFor / (24 * time.Hour))
	cli.Success("manifest.json and manifest.json.sig written to %s", f.out)
	cli.Info("publish both files at the feed URL the app UpdateConfig points at")
	cli.Info("the manifest expires in %d days; run this command again before then, or apps refuse the feed", days)
}

const maxFeedDays = int(desktop.MaxFeedLifetime / (24 * time.Hour))

func feedLifetime(validDays string) (time.Duration, error) {
	if validDays == "" {
		return 0, nil
	}
	days, err := strconv.Atoi(validDays)
	if err != nil || days < 1 || days > maxFeedDays {
		return 0, fmt.Errorf("--valid-days %q must be a whole number of days from 1 to %d", validDays, maxFeedDays)
	}
	return time.Duration(days) * 24 * time.Hour, nil
}
