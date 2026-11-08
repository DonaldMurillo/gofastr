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
	errUpdateInProgress    = &Error{Code: CodeInvalidInput, Message: "an update install is already running"}
	errUpdateInstalled     = &Error{Code: CodeInvalidInput, Message: "the update is installed; the app is restarting"}

	// errUpdateRunningVersion is Unsupported, not Internal: no feed can
	// fix a bundle built with a version the updater cannot compare, so
	// the scheduled loop stops instead of failing every interval.
	errUpdateRunningVersion = &Error{Code: CodeUnsupported, Message: "this app's version is not major.minor.patch; rebuild it with a semantic --version"}
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
	// rename is the swap's rename, a test seam for failing the second
	// rename and its rollback. nil selects os.Rename.
	rename func(oldpath, newpath string) error

	// platformKey is the feed entry key, a test seam; "" selects this
	// host's GOOS-GOARCH.
	platformKey string

	// defaultOnce builds the OS platform once: the Windows one reads the
	// running executable's version resource, which does not change.
	defaultOnce     sync.Once
	defaultPlatform update.Platform

	mu        sync.Mutex
	notified  string // last version successfully delivered to every open page
	notifying bool   // an event delivery is in flight, guarded by mu

	// installMu single-flights install: two runs would share
	// <Name>.app.new and .old, and the second's RemoveAll(old) could
	// delete the first's only copy of the running bundle between its
	// renames. A second call while one runs is refused, never queued.
	installMu sync.Mutex
	// installed is set once a swap succeeds: the process is quitting
	// and the running version is still the old one, so a later call
	// would reinstall over the new bundle. Guarded by installMu.
	installed bool
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

// renameFile is the swap's rename: the injected one, or os.Rename.
func (u *updater) renameFile(oldpath, newpath string) error {
	if u.rename != nil {
		return u.rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

// platformOrDefault returns the injected platform or the OS default
// (NSBundle-backed on darwin through objc.Main, the executable's version
// resource on Windows, refusing elsewhere).
func (u *updater) platformOrDefault() update.Platform {
	if u.platform != nil {
		return u.platform
	}
	u.defaultOnce.Do(func() {
		p := update.DefaultPlatform(nil, update.ExecRunner{})
		if w, ok := p.(interface{ SetWarn(func(string)) }); ok {
			w.SetWarn(func(msg string) { u.b.logger.Warn("desktop: " + msg) })
		}
		u.defaultPlatform = p
	})
	return u.defaultPlatform
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
	// A running version the updater cannot parse is the bundle's fault,
	// never the feed's: report it as such instead of "the feed is
	// invalid" on every check.
	if _, err := update.ParseVersion(running); err != nil {
		u.b.logger.Warn("desktop: running app version is not a semantic version; updates are off")
		return resolved{}, errUpdateRunningVersion
	}
	newer, err := update.IsNewer(rel.Version, running)
	if err != nil {
		u.b.logger.Warn("desktop: update feed version comparison failed", "error", err.Error())
		return resolved{}, errUpdateFeedBad
	}
	r := resolved{release: rel, newer: newer}
	if newer {
		key := u.platformKey
		if key == "" {
			key = update.PlatformKey()
		}
		entry, err := rel.Entry(key)
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
// zip, then codesign on macOS or Authenticode on Windows), swaps it in
// place of the running bundle, relaunches
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

// install is InstallUpdate's body. One install runs at a time per
// battery; a concurrent call gets errUpdateInProgress, and every call
// after a successful swap gets errUpdateInstalled.
func (u *updater) install(ctx context.Context) error {
	if !u.installMu.TryLock() {
		return errUpdateInProgress
	}
	defer u.installMu.Unlock()
	if u.installed {
		return errUpdateInstalled
	}
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
		u.b.logger.Error("desktop: update staging directory failed", "error", update.LogDetail(err))
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
	// The bundle must be the release the signed feed names, for this
	// app: a 0.1.0 build labelled 1.2.0 would reinstall on every check,
	// an old archive under a new label is a silent downgrade, and
	// another app's bundle must never replace this one. The identity
	// values stay out of the log (they come from the download).
	units, inspect := installLayout(platform, name, bundleDir)
	info, err := inspect(newApp)
	if err != nil {
		u.b.logger.Warn("desktop: update bundle identity refused", "error", err.Error())
		return errUpdateArchive
	}
	if info.ShortVersion != r.release.Version {
		u.b.logger.Warn("desktop: update bundle version does not match the feed",
			"feed_version", r.release.Version) // semver-validated: no control bytes
		return errUpdateArchive
	}
	if id := platform.Identifier(); id == "" || info.Identifier != id {
		u.b.logger.Warn("desktop: update bundle identifier does not match the running app")
		return errUpdateArchive
	}
	if err := platform.VerifySignature(newApp); err != nil {
		u.b.logger.Warn("desktop: update bundle signature check failed", "error", err.Error())
		return errUpdateArchive
	}
	if !u.swap(newApp, units) {
		return errUpdateApply
	}
	// The new bundle is in place: from here a second install would
	// swap over it, relaunched or not.
	u.installed = true
	// Relaunch errors are runner output, which update.ExecRunner has
	// already rewritten to name the bundle, not its path.
	if err := platform.Relaunch(units[0].Current); err != nil {
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

// installLayout returns the paths an install replaces and the reader
// for the extracted bundle's identity. A FileLayout platform (Windows)
// names its own files; every other platform swaps the one bundle
// directory, <dir>/<name>, and reads its Info.plist.
func installLayout(platform update.Platform, name, bundleDir string) ([]update.Unit, func(string) (update.BundleInfo, error)) {
	if fl, ok := platform.(update.FileLayout); ok {
		if units, ok := fl.Units(); ok && len(units) > 0 {
			return units, fl.Inspect
		}
	}
	return []update.Unit{{Current: filepath.Join(bundleDir, name)}}, update.ReadBundleInfo
}

// swap moves the extracted bundle's units into place. Each unit's new
// copy goes beside its installed path first (<path>.new, same volume),
// then two renames per unit: installed to <path>.old, new into place.
// A failed rename moves every unit already swapped back. A crash
// between the two renames of a unit leaves no installed copy of it,
// only <path>.old and <path>.new beside it: the app cannot start to
// repair that, so the user renames one back. After a completed swap
// the next launch's startUpdater removes the .old copies. It reports
// whether the swap completed; every failure is logged here.
func (u *updater) swap(newApp string, units []update.Unit) bool {
	// A rollback that failed leaves <path>.old as the only copy of the
	// app. The RemoveAll(old) below would delete it, so a swap with a
	// missing installed path refuses before touching anything.
	for _, unit := range units {
		if _, err := os.Lstat(unit.Current); err != nil {
			u.b.logger.Error("desktop: the app bundle is missing, so the update was not applied; rename the .old copy beside it back", "error", update.LogDetail(err))
			return false
		}
	}
	for _, unit := range units {
		beside := unit.Current + ".new"
		if err := os.RemoveAll(beside); err != nil {
			u.b.logger.Error("desktop: clearing the previous staged bundle failed", "error", update.LogDetail(err))
			return false
		}
		if err := update.MoveTree(filepath.Join(newApp, unit.Rel), beside); err != nil {
			u.b.logger.Error("desktop: moving the new bundle into place failed", "error", update.LogDetail(err))
			return false
		}
	}
	for _, unit := range units {
		if err := os.RemoveAll(unit.Current + ".old"); err != nil {
			u.b.logger.Error("desktop: clearing the leftover old bundle failed", "error", update.LogDetail(err))
			return false
		}
	}
	for i, unit := range units {
		old, beside := unit.Current+".old", unit.Current+".new"
		if err := u.renameFile(unit.Current, old); err != nil {
			u.b.logger.Error("desktop: moving the current bundle aside failed", "error", update.LogDetail(err))
			u.rollback(units[:i])
			return false
		}
		if err := u.renameFile(beside, unit.Current); err != nil {
			u.b.logger.Error("desktop: moving the new bundle into place failed", "error", update.LogDetail(err))
			if rb := u.renameFile(old, unit.Current); rb != nil {
				u.b.logger.Error("desktop: ROLLBACK FAILED; the app bundle is missing, rename the .old copy beside it back", "error", update.LogDetail(rb))
				return false
			}
			u.rollback(units[:i])
			return false
		}
	}
	return true
}

// rollback undoes completed unit swaps, last first: the new copy goes
// back to <path>.new and <path>.old returns to the installed path.
func (u *updater) rollback(done []update.Unit) {
	for i := len(done) - 1; i >= 0; i-- {
		cur := done[i].Current
		if err := u.renameFile(cur, cur+".new"); err != nil {
			u.b.logger.Error("desktop: ROLLBACK FAILED; the app bundle is half updated", "error", update.LogDetail(err))
			return
		}
		if err := u.renameFile(cur+".old", cur); err != nil {
			u.b.logger.Error("desktop: ROLLBACK FAILED; the app bundle is missing, rename the .old copy beside it back", "error", update.LogDetail(err))
			return
		}
	}
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

// removeStaleOld deletes the copies a previous update left beside the
// running app as <path>.old (the old process could not remove itself
// while running).
func (u *updater) removeStaleOld() {
	platform := u.platformOrDefault()
	name, dir, ok := platform.Bundle()
	if !ok {
		return
	}
	units, _ := installLayout(platform, name, dir)
	removed := false
	for _, unit := range units {
		old := unit.Current + ".old"
		if _, err := os.Lstat(old); err != nil {
			continue
		}
		if err := os.RemoveAll(old); err != nil {
			u.b.logger.Warn("desktop: removing the previous update's leftover bundle failed", "error", update.LogDetail(err))
			continue
		}
		removed = true
	}
	if removed {
		u.b.logger.Info("desktop: removed the previous update's leftover bundle")
	}
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
	if u.notified == res.Version || u.notifying {
		u.mu.Unlock()
		return
	}
	u.notifying = true
	u.mu.Unlock()

	// Eval invokes the native window outside the state lock. Only a
	// successful delivery consumes the version; a closed or unready
	// window must get another chance on the next scheduled check.
	defer func() {
		u.mu.Lock()
		u.notifying = false
		u.mu.Unlock()
	}()
	if err := u.b.Emit("update_available", map[string]string{"version": res.Version, "notes": res.Notes}); err != nil {
		u.b.logger.Warn("desktop: delivering the update_available event failed", "error", err.Error())
		return
	}
	u.mu.Lock()
	u.notified = res.Version
	u.mu.Unlock()
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

// ValidateAppVersion reports whether version is one the updater can
// compare: semver major.minor.patch with an optional pre-release, no
// build metadata, no leading zeros. `gofastr desktop build` refuses a
// macOS --version that fails it, since the bundle's
// CFBundleShortVersionString is the running version every update check
// compares against the feed.
func ValidateAppVersion(version string) error {
	_, err := update.ParseVersion(version)
	return err
}
