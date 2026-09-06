package desktop

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/update"
)

// Auto-update: a signed JSON feed names the newest version per
// platform; the battery checks it on a schedule, downloads and verifies
// the archive, swaps the bundle, and relaunches. The engine lives in
// internal/update (feed verification, semver, capped downloads, zip
// extraction, the platform seam); this file is the battery surface:
// UpdateConfig, CheckForUpdates, InstallUpdate, the scheduled loop,
// and the `gofastr desktop feed` writer re-export.

// UpdateConfig enables the auto-updater. Zero value: none.
type UpdateConfig struct {
	// FeedURL is the manifest's own URL ("https://example.com/notes/
	// manifest.json"); the detached signature is fetched from
	// FeedURL + ".sig". https only; plain http is accepted for
	// 127.0.0.1 so tests can serve the feed locally.
	FeedURL string

	// PublicKey is the feed's ed25519 public key as 64 hex chars
	// (32 bytes). `gofastr desktop keygen` mints the pair.
	PublicKey string

	// Interval is the scheduled check period. Zero means the default
	// (6 h); negative means never check automatically (manual
	// CheckForUpdates and updates.install still work).
	Interval time.Duration

	// Channel optionally selects feed.channels[channel] when the feed
	// carries it; the feed's top level otherwise.
	Channel string
}

// Update is the outcome of one check. Version and Notes describe the
// release the feed names, whether or not it is newer.
type Update struct {
	Available bool
	Version   string
	Notes     string
}

// Update tuning knobs.
const (
	// defaultUpdateInterval is UpdateConfig.Interval's default.
	defaultUpdateInterval = 6 * time.Hour
	// firstUpdateCheckDelay keeps the scheduled first check off the
	// boot path.
	firstUpdateCheckDelay = 30 * time.Second
	// updateFeedTimeout bounds one manifest fetch.
	updateFeedTimeout = 30 * time.Second
	// updateDownloadTimeout bounds one archive download.
	updateDownloadTimeout = 10 * time.Minute
	// updateQuitDelay lets the bridge flush the install response
	// before the shell quits the process.
	updateQuitDelay = 250 * time.Millisecond
	// updateNotesLimit bounds the notes string handed to the page.
	updateNotesLimit = 8 << 10
)

// The updater's fixed bridge errors. None carries a path or URL; the
// real error is logged server-side at the failure site.
var (
	errUpdateNotConfigured = &Error{Code: CodeUnsupported, Message: "auto-update is not configured"}
	errUpdateNeedsBundle   = &Error{Code: CodeUnsupported, Message: "updates need an app bundle; run from gofastr desktop build output"}
	errUpdateFeedFetch     = &Error{Code: CodeInternal, Message: "the update feed could not be fetched"}
	errUpdateFeedBad       = &Error{Code: CodeInternal, Message: "the update feed is invalid"}
	errUpdateNoEntry       = &Error{Code: CodeInternal, Message: "the update feed has no entry for this platform"}
	errUpdateArchive       = &Error{Code: CodeInternal, Message: "the update archive failed verification"}
	errUpdateApply         = &Error{Code: CodeInternal, Message: "the update could not be applied"}
	errUpdateNotAvailable  = &Error{Code: CodeInvalidInput, Message: "no newer version is available"}
)

// validateUpdateConfig is Config.Update's construction-time check New
// runs: a wiring error belongs at construction, like a bad menu.
func validateUpdateConfig(cfg *UpdateConfig) error {
	if !update.AllowedURL(cfg.FeedURL) {
		return errors.New("desktop: Config.Update.FeedURL must be the manifest's https URL (plain http only to 127.0.0.1)")
	}
	raw, err := hex.DecodeString(cfg.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return errors.New("desktop: Config.Update.PublicKey must be 64 hex chars (a 32-byte ed25519 public key)")
	}
	if n := len(cfg.Channel); n > 64 {
		return fmt.Errorf("desktop: Config.Update.Channel is %d chars, at most 64", n)
	}
	for i := range len(cfg.Channel) {
		if cfg.Channel[i] < 0x20 || cfg.Channel[i] == 0x7f {
			return errors.New("desktop: Config.Update.Channel must not carry control characters")
		}
	}
	return nil
}

// updater is one battery's update state (Battery.updater).
type updater struct {
	b    *Battery
	pub  ed25519.PublicKey
	http *http.Client
	// platform is the test seam: set before the first check to
	// observe the apply step. nil selects the OS default.
	platform update.Platform

	mu       sync.Mutex
	notified string // last version an update_available event named
}

// updaterFor returns (creating once) the battery's updater. The public
// key was validated at New, so a decode failure here leaves pub nil
// and every check refuses with the not-configured error.
func (b *Battery) updaterFor() *updater {
	b.updaterOnce.Do(func() {
		u := &updater{b: b, http: &http.Client{}}
		if b.cfg.Update != nil {
			if raw, err := hex.DecodeString(b.cfg.Update.PublicKey); err == nil && len(raw) == ed25519.PublicKeySize {
				u.pub = ed25519.PublicKey(raw)
			}
		}
		b.updater = u
	})
	return b.updater
}

// platformOrNil returns the test-injected platform, or nil.
func (u *updater) platformOverride() update.Platform { return u.platform }

// platformOrDefault returns the injected platform or the OS default
// (NSBundle-backed on darwin through objc.Main, refusing elsewhere).
func (u *updater) platformOrDefault() update.Platform {
	if u.platform != nil {
		return u.platform
	}
	return update.DefaultPlatform(nil, update.ExecRunner{})
}

// CheckForUpdates runs one check against the configured feed now.
// It reports ErrUnsupported when auto-update is not configured or the
// app is not running from a bundle (an unbundled run never updates).
func (b *Battery) CheckForUpdates(ctx context.Context) (Update, error) {
	if b.cfg.Update == nil {
		return Update{}, errUpdateNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return Update{}, ErrCancelled
	}
	r, err := b.updaterFor().resolve(ctx)
	if err != nil {
		return Update{}, err
	}
	return Update{Available: r.newer, Version: r.release.Version, Notes: clipNotes(r.release.Notes)}, nil
}

// clipNotes bounds the feed's notes before they reach the page.
func clipNotes(notes string) string {
	if len(notes) <= updateNotesLimit {
		return notes
	}
	return notes[:updateNotesLimit]
}

// resolved is one verified look at the feed.
type resolved struct {
	release update.Release
	entry   update.PlatformEntry
	newer   bool
}

// resolve fetches and verifies the feed, selects the configured
// channel, compares against the running version, and (when newer)
// resolves this platform's entry. Nothing it returns or logs carries
// the feed URL: the sentinel errors' text is fixed words, by design
// (the controlbytes posture applied at the source).
func (u *updater) resolve(ctx context.Context) (resolved, error) {
	if u.pub == nil {
		return resolved{}, errUpdateNotConfigured
	}
	cfg := u.b.cfg.Update
	fctx, cancel := context.WithTimeout(ctx, updateFeedTimeout)
	defer cancel()
	manifest, err := update.Fetch(fctx, u.http, cfg.FeedURL, update.MaxManifestSize)
	if err != nil {
		u.b.logger.Warn("desktop: update feed fetch failed", "error", err.Error())
		return resolved{}, errUpdateFeedFetch
	}
	sig, err := update.Fetch(fctx, u.http, cfg.FeedURL+".sig", update.MaxManifestSize)
	if err != nil {
		u.b.logger.Warn("desktop: update signature fetch failed", "error", err.Error())
		return resolved{}, errUpdateFeedFetch
	}
	feed, err := update.ParseFeed(manifest, sig, u.pub)
	if err != nil {
		u.b.logger.Warn("desktop: update feed rejected", "error", err.Error())
		return resolved{}, errUpdateFeedBad
	}
	rel := feed.Select(cfg.Channel)
	running := u.platformOrDefault().Version()
	if running == "" {
		// An unbundled run has no version to compare and never
		// updates.
		return resolved{}, errUpdateNeedsBundle
	}
	newer, err := update.IsNewer(rel.Version, running)
	if err != nil {
		u.b.logger.Warn("desktop: update feed version comparison failed", "error", err.Error())
		return resolved{}, errUpdateFeedBad
	}
	r := resolved{release: rel, newer: newer}
	if newer {
		entry, err := rel.Entry(update.PlatformKey())
		if err != nil {
			return resolved{}, errUpdateNoEntry
		}
		if entry.Size <= 0 || entry.Size > update.MaxArchiveSize {
			return resolved{}, errUpdateFeedBad
		}
		r.entry = entry
	}
	return r, nil
}

// InstallUpdate downloads the newest version from the configured feed,
// verifies it (ed25519 feed signature, sha256, size, single-bundle
// zip, codesign), swaps it in place of the running bundle, relaunches
// it, and quits the current process. ErrUnsupported when unbundled or
// on a host without an apply step.
func (b *Battery) InstallUpdate(ctx context.Context) error {
	if b.cfg.Update == nil {
		return errUpdateNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return ErrCancelled
	}
	return b.updaterFor().install(ctx)
}

// install is InstallUpdate's body.
func (u *updater) install(ctx context.Context) error {
	platform := u.platformOrDefault()
	name, bundleDir, ok := platform.Bundle()
	if !ok {
		return errUpdateNeedsBundle
	}
	r, err := u.resolve(ctx)
	if err != nil {
		return err
	}
	if !r.newer {
		return errUpdateNotAvailable
	}

	// Everything lands under a 0700 staging directory in the app's
	// data dir and is removed on ANY exit path (success moves the
	// bundle out first; failure leaves nothing behind).
	stage, err := u.stageDir()
	if err != nil {
		u.b.logger.Error("desktop: update staging directory failed", "error", err.Error())
		return errUpdateApply
	}
	defer os.RemoveAll(stage)

	dctx, cancel := context.WithTimeout(ctx, updateDownloadTimeout)
	defer cancel()
	data, err := update.Fetch(dctx, u.http, r.entry.URL, r.entry.Size)
	if err != nil {
		u.b.logger.Warn("desktop: update archive download failed", "error", err.Error())
		return errUpdateArchive
	}
	if err := update.VerifyArchive(data, r.entry); err != nil {
		u.b.logger.Warn("desktop: update archive rejected", "error", err.Error())
		return errUpdateArchive
	}
	newApp, err := update.ExtractZip(data, stage, name)
	if err != nil {
		u.b.logger.Warn("desktop: update archive extraction refused", "error", err.Error())
		return errUpdateArchive
	}
	if err := platform.VerifySignature(newApp); err != nil {
		u.b.logger.Warn("desktop: update bundle signature check failed", "error", err.Error())
		return errUpdateArchive
	}

	// Swap: beside the running bundle first (same volume), then the
	// two renames, then relaunch and quit. A crash between the
	// renames leaves the old bundle at <Name>.app.old, which the next
	// launch's startUpdater removes.
	beside := filepath.Join(bundleDir, name+".new")
	if err := os.RemoveAll(beside); err != nil {
		u.b.logger.Error("desktop: clearing the previous staged bundle failed", "error", err.Error())
		return errUpdateApply
	}
	if err := update.MoveTree(newApp, beside); err != nil {
		u.b.logger.Error("desktop: moving the new bundle into place failed", "error", err.Error())
		return errUpdateApply
	}
	current := filepath.Join(bundleDir, name)
	old := current + ".old"
	if err := os.RemoveAll(old); err != nil {
		u.b.logger.Error("desktop: clearing the leftover old bundle failed", "error", err.Error())
		return errUpdateApply
	}
	if err := os.Rename(current, old); err != nil {
		u.b.logger.Error("desktop: moving the current bundle aside failed", "error", err.Error())
		return errUpdateApply
	}
	if err := os.Rename(beside, current); err != nil {
		u.b.logger.Error("desktop: moving the new bundle into place failed", "error", err.Error())
		if rb := os.Rename(old, current); rb != nil {
			u.b.logger.Error("desktop: ROLLBACK FAILED; the app bundle is missing, restore it from "+name+".old", "error", rb.Error())
		}
		return errUpdateApply
	}
	if err := platform.Relaunch(current); err != nil {
		u.b.logger.Error("desktop: relaunching the updated bundle failed", "error", err.Error())
		return errUpdateApply
	}
	// Give the bridge response a moment to flush, then quit; the new
	// instance is already running.
	time.AfterFunc(updateQuitDelay, u.b.shell.Quit)
	u.b.logger.Info("desktop: update installed, relaunching",
		"version", r.release.Version) // semver-validated: no control bytes
	return nil
}

// stageDir creates the 0700 staging directory under the battery's data
// dir.
func (u *updater) stageDir() (string, error) {
	dir := u.b.dataDir
	if dir == "" {
		var err error
		dir, err = DataDir(u.b.cfg.ID)
		if err != nil {
			return "", err
		}
	}
	return os.MkdirTemp(dir, "update-") // 0700
}

// startUpdater runs from Run once the app is listening; the context
// ends when Run returns. It clears a previous update's leftover .old
// bundle, then schedules checks (never when Interval is negative).
func (b *Battery) startUpdater(ctx context.Context) {
	if b.cfg.Update == nil {
		return
	}
	u := b.updaterFor()
	u.removeStaleOld()
	if b.cfg.Update.Interval < 0 {
		return
	}
	interval := b.cfg.Update.Interval
	if interval == 0 {
		interval = defaultUpdateInterval
	}
	go u.loop(ctx, interval)
}

// removeStaleOld deletes the bundle a previous update left beside the
// running one (the old process could not remove itself while running).
func (u *updater) removeStaleOld() {
	name, dir, ok := u.platformOrDefault().Bundle()
	if !ok {
		return
	}
	old := filepath.Join(dir, name+".old")
	if _, err := os.Stat(old); err != nil {
		return
	}
	if err := os.RemoveAll(old); err != nil {
		u.b.logger.Warn("desktop: removing the previous update's leftover bundle failed", "error", err.Error())
		return
	}
	u.b.logger.Info("desktop: removed the previous update's leftover bundle")
}

// loop runs the scheduled checks: first after firstUpdateCheckDelay,
// then every interval, until the Run context ends. An unsupported host
// (unbundled dev run, non-darwin) stops the loop after one look so the
// log is not spammed; every other failure is a Warn and retries next
// interval. The check body is recover-guarded: a panic in a scheduled
// goroutine must never take the app down (the recovercallback shape).
func (u *updater) loop(ctx context.Context, interval time.Duration) {
	timer := time.NewTimer(firstUpdateCheckDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if u.checkAndNotify(ctx) {
			return
		}
		timer.Reset(interval)
	}
}

// checkAndNotify runs one scheduled check. It reports whether the loop
// should stop.
func (u *updater) checkAndNotify(ctx context.Context) (stop bool) {
	defer func() {
		if r := recover(); r != nil {
			u.b.logger.Warn("desktop: scheduled update check panicked; recovered")
		}
	}()
	res, err := u.check(ctx)
	if err != nil {
		var derr *Error
		if errors.As(err, &derr) && derr.Code == CodeUnsupported {
			u.b.logger.Info("desktop: auto-update is unavailable on this host; scheduled checks stopped")
			return true
		}
		u.b.logger.Warn("desktop: scheduled update check failed", "error", err.Error())
		return false
	}
	if !res.Available {
		// The version is grammar-validated semver: no control bytes
		// reach the log.
		u.b.logger.Info("desktop: app is up to date", "feed_version", res.Version)
		return false
	}
	u.notifyOnce(res)
	return false
}

// check is CheckForUpdates without the nil-config guard (the loop only
// runs with a config).
func (u *updater) check(ctx context.Context) (Update, error) {
	r, err := u.resolve(ctx)
	if err != nil {
		return Update{}, err
	}
	return Update{Available: r.newer, Version: r.release.Version, Notes: clipNotes(r.release.Notes)}, nil
}

// notifyOnce emits update_available to the page, once per version (a
// still-uninstalled update does not re-toast every interval).
func (u *updater) notifyOnce(res Update) {
	u.mu.Lock()
	first := u.notified != res.Version
	u.notified = res.Version
	u.mu.Unlock()
	if !first {
		return
	}
	if err := u.b.Emit("update_available", map[string]string{"version": res.Version, "notes": res.Notes}); err != nil {
		u.b.logger.Warn("desktop: delivering the update_available event failed", "error", err.Error())
	}
	u.b.logger.Info("desktop: update available", "version", res.Version)
}

// SignUpdateFeed builds the signed feed pair the updater verifies:
// manifest.json plus manifest.json.sig for one platform archive, with
// the sha256 and size computed from the archive file. It is the
// `gofastr desktop feed` verb's engine, exported so the CLI and app
// tests share the exact format with the verifier.
func SignUpdateFeed(outDir, keyPath, version, notes, platform, archivePath, archiveURL string) error {
	return update.WriteFeed(outDir, keyPath, version, notes, platform, archivePath, archiveURL)
}
