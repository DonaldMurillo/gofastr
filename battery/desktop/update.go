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
	"runtime"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/update"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
)

// UpdateConfig enables the auto-updater.
type UpdateConfig struct {
	// FeedURL is the manifest's https URL; the signature is fetched from FeedURL + ".sig".
	FeedURL string

	// PublicKey is the feed's ed25519 public key as 64 hex chars (32 bytes).
	PublicKey string

	// Interval is the scheduled check period: zero means 6 h, negative never.
	Interval time.Duration

	// Channel optionally selects feed.channels[channel] when the feed carries it; the feed's top level otherwise.
	Channel string

	// MacOSTeamID pins updates to this Apple Developer Team's Developer ID certificate.
	MacOSTeamID string

	// MacOSAllowAdHoc lets an app with no signing identity update.
	MacOSAllowAdHoc bool
}

// Update is the outcome of one check.
type Update struct {
	Available bool
	Version   string
	Notes     string
}

const (
	defaultUpdateInterval = 6 * time.Hour
	firstUpdateCheckDelay = 30 * time.Second
	updateFeedTimeout     = 30 * time.Second
	updateDownloadTimeout = 10 * time.Minute
	updateQuitDelay       = 250 * time.Millisecond
	updateNotesLimit      = 8 << 10
)

var (
	errUpdateNotConfigured = &Error{Code: CodeUnsupported, Message: "auto-update is not configured"}
	errUpdateNeedsBundle   = &Error{Code: CodeUnsupported, Message: "updates need an app bundle; run from gofastr desktop build output, installed with gofastr desktop install on Linux"}
	errUpdateFeedFetch     = &Error{Code: CodeInternal, Message: "the update feed could not be fetched"}
	errUpdateFeedBad       = &Error{Code: CodeInternal, Message: "the update feed is invalid"}
	errUpdateFeedExpired   = &Error{Code: CodeInternal, Message: "the update feed has expired"}
	errUpdateNoEntry       = &Error{Code: CodeInternal, Message: "the update feed has no entry for this platform"}
	errUpdateArchive       = &Error{Code: CodeInternal, Message: "the update archive failed verification"}
	errUpdateApply         = &Error{Code: CodeInternal, Message: "the update could not be applied"}
	errUpdateNotAvailable  = &Error{Code: CodeInvalidInput, Message: "no newer version is available"}
	errUpdateInProgress    = &Error{Code: CodeInvalidInput, Message: "an update install is already running"}
	errUpdateInstalled     = &Error{Code: CodeInvalidInput, Message: "the update is installed; the app is restarting"}
	errUpdateRestart       = &Error{Code: CodeInvalidInput, Message: "another window of this app installed an update; restart the app to use it"}

	errUpdateRunningVersion = &Error{Code: CodeUnsupported, Message: "this app's version is not major.minor.patch; rebuild it with a semantic --version"}
)

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
	if cfg.MacOSTeamID != "" {
		if !validTeamID(cfg.MacOSTeamID) {
			return errors.New("desktop: Config.Update.MacOSTeamID must be an Apple Team ID: ten uppercase letters and digits")
		}
		if cfg.MacOSAllowAdHoc {
			return errors.New("desktop: Config.Update.MacOSTeamID and MacOSAllowAdHoc cannot both be set: one pins the signer, the other accepts any")
		}
	}
	return nil
}

func validTeamID(id string) bool {
	if len(id) != 10 {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

type updater struct {
	b        *Battery
	pub      ed25519.PublicKey
	http     *http.Client
	platform update.Platform
	rename   func(oldpath, newpath string) error

	platformKey string

	defaultOnce     sync.Once
	defaultPlatform update.Platform

	mu        sync.Mutex
	notified  string // last version successfully delivered to every open page
	notifying bool   // an event delivery is in flight, guarded by mu

	installMu sync.Mutex
	installed bool
}

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

func (u *updater) renameFile(oldpath, newpath string) error {
	if u.rename != nil {
		return u.rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

func (u *updater) platformOrDefault() update.Platform {
	if u.platform != nil {
		return u.platform
	}
	u.defaultOnce.Do(func() {
		p := update.DefaultPlatform(nil, update.ExecRunner{})
		u.configurePlatform(p)
		u.defaultPlatform = p
	})
	return u.defaultPlatform
}

func (u *updater) configurePlatform(p update.Platform) {
	if w, ok := p.(interface{ SetWarn(func(string)) }); ok {
		w.SetWarn(func(msg string) { u.b.logger.Warn("desktop: " + msg) })
	}
	s, ok := p.(interface {
		SetSignaturePolicy(update.SignaturePolicy)
	})
	if cfg := u.b.cfg.Update; ok && cfg != nil {
		s.SetSignaturePolicy(update.SignaturePolicy{TeamID: cfg.MacOSTeamID, AllowAdHoc: cfg.MacOSAllowAdHoc})
	}
}

// CheckForUpdates runs one check against the configured feed now.
func (b *Battery) CheckForUpdates(ctx context.Context) (Update, error) {
	if b.cfg.Update == nil {
		return Update{}, errUpdateNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return Update{}, ErrCancelled
	}
	return b.updaterFor().check(ctx)
}

func clipNotes(notes string) string {
	if len(notes) <= updateNotesLimit {
		return notes
	}
	return notes[:updateNotesLimit]
}

type resolved struct {
	release update.Release
	entry   update.PlatformEntry
	newer   bool
}

func (u *updater) resolve(ctx context.Context, platform update.Platform) (resolved, error) {
	if u.pub == nil {
		return resolved{}, errUpdateNotConfigured
	}
	running := platform.Version()
	if running == "" {
		return resolved{}, errUpdateNeedsBundle
	}
	if _, err := update.ParseVersion(running); err != nil {
		u.b.logger.Warn("desktop: running app version is not a semantic version; updates are off")
		return resolved{}, errUpdateRunningVersion
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
	feed, err := update.ParseFeed(manifest, sig, u.pub, time.Now())
	if errors.Is(err, update.ErrFeedExpired) {
		u.b.logger.Warn("desktop: update feed has expired; the publisher signs it again with gofastr desktop feed")
		return resolved{}, errUpdateFeedExpired
	}
	if err != nil {
		u.b.logger.Warn("desktop: update feed rejected", "error", err.Error())
		return resolved{}, errUpdateFeedBad
	}
	rel := feed.Select(cfg.Channel)
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

// InstallUpdate downloads, verifies and swaps in the newest version, relaunches it and quits; unsupported when unbundled.
func (b *Battery) InstallUpdate(ctx context.Context) error {
	if b.cfg.Update == nil {
		return errUpdateNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return ErrCancelled
	}
	return b.updaterFor().install(ctx)
}

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
	lock, locked, err := u.lockInstall()
	if err != nil {
		u.b.logger.Error("desktop: the update lock could not be taken", "error", update.LogDetail(err))
		return errUpdateApply
	}
	if !locked {
		return errUpdateInProgress
	}
	defer lock.Close()
	r, err := u.resolve(ctx, platform)
	if err != nil {
		return err
	}
	if !r.newer {
		return errUpdateNotAvailable
	}

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
	newApp, err := extractBundle(platform, data, stage, name)
	if errors.Is(err, update.ErrInstallationChanged) {
		return errUpdateRestart
	}
	if err != nil {
		u.b.logger.Warn("desktop: update archive extraction refused", "error", err.Error())
		return errUpdateArchive
	}
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
	u.installed = true
	if err := platform.Relaunch(units[0].Current); err != nil {
		u.b.logger.Error("desktop: relaunching the updated bundle failed", "error", err.Error())
		return errUpdateApply
	}
	time.AfterFunc(updateQuitDelay, u.b.shell.Quit)
	u.b.logger.Info("desktop: update installed, relaunching",
		"version", r.release.Version) // semver-validated: no control bytes
	return nil
}

func extractBundle(platform update.Platform, data []byte, stage, name string) (string, error) {
	if ex, ok := platform.(update.Extractor); ok {
		return ex.Extract(data, stage)
	}
	return update.ExtractZip(data, stage, name)
}

func installLayout(platform update.Platform, name, bundleDir string) ([]update.Unit, func(string) (update.BundleInfo, error)) {
	if fl, ok := platform.(update.FileLayout); ok {
		if units, ok := fl.Units(); ok && len(units) > 0 {
			return units, fl.Inspect
		}
	}
	return []update.Unit{{Current: filepath.Join(bundleDir, name)}}, update.ReadBundleInfo
}

func (u *updater) swap(newApp string, units []update.Unit) bool {
	for _, unit := range units {
		if _, err := os.Lstat(unit.Current); err != nil {
			u.b.logger.Error("desktop: the app bundle is missing, so the update was not applied; rename the copy beside it ending in old back", "error", update.LogDetail(err))
			return false
		}
	}
	for _, unit := range units {
		beside := unit.New()
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
		if err := os.RemoveAll(unit.Old()); err != nil {
			u.b.logger.Error("desktop: clearing the leftover old bundle failed", "error", update.LogDetail(err))
			return false
		}
	}
	for i, unit := range units {
		old, beside := unit.Old(), unit.New()
		if err := u.renameFile(unit.Current, old); err != nil {
			u.b.logger.Error("desktop: moving the current bundle aside failed", "error", update.LogDetail(err))
			u.rollback(units[:i])
			return false
		}
		if err := u.renameFile(beside, unit.Current); err != nil {
			u.b.logger.Error("desktop: moving the new bundle into place failed", "error", update.LogDetail(err))
			if rb := u.renameFile(old, unit.Current); rb != nil {
				u.b.logger.Error("desktop: ROLLBACK FAILED; the app bundle is missing, rename the copy beside it ending in old back", "error", update.LogDetail(rb))
				return false
			}
			u.rollback(units[:i])
			return false
		}
	}
	return true
}

func (u *updater) rollback(done []update.Unit) {
	for i := len(done) - 1; i >= 0; i-- {
		cur := done[i].Current
		if err := u.renameFile(cur, done[i].New()); err != nil {
			u.b.logger.Error("desktop: ROLLBACK FAILED; the app bundle is half updated", "error", update.LogDetail(err))
			return
		}
		if err := u.renameFile(done[i].Old(), cur); err != nil {
			u.b.logger.Error("desktop: ROLLBACK FAILED; the app bundle is missing, rename the copy beside it ending in old back", "error", update.LogDetail(err))
			return
		}
	}
}

func (u *updater) dataDir() (string, error) {
	if u.b.dataDir != "" {
		return u.b.dataDir, nil
	}
	return DataDir(u.b.cfg.ID)
}

func (u *updater) stageDir() (string, error) {
	dir, err := u.dataDir()
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, "update-") // 0700
}

// updateLockName is the file in the data directory every running
// instance of the app locks for the length of an install.
const updateLockName = "update.lock"

// lockInstall takes the cross-process install lock without waiting;
// false means another instance holds it.
func (u *updater) lockInstall() (*os.File, bool, error) {
	dir, err := u.dataDir()
	if err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(filepath.Join(dir, updateLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	ok, err := filelock.TryLock(f)
	if err != nil || !ok {
		_ = f.Close()
		return nil, false, err
	}
	return f, true, nil
}

func (b *Battery) startUpdater(ctx context.Context) {
	if b.cfg.Update == nil {
		return
	}
	u := b.updaterFor()
	if b.cfg.Update.MacOSAllowAdHoc && runtime.GOOS == "darwin" {
		b.logger.Warn("desktop: Config.Update.MacOSAllowAdHoc is set; an app with no signing identity installs any validly signed update, on the feed signature alone")
	}
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

func (u *updater) removeStaleOld() {
	platform := u.platformOrDefault()
	name, dir, ok := platform.Bundle()
	if !ok {
		return
	}
	units, _ := installLayout(platform, name, dir)
	removed := false
	for _, unit := range units {
		old := unit.Old()
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

// stops the loop after one look so the
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
		u.b.logger.Info("desktop: app is up to date", "feed_version", res.Version)
		return false
	}
	u.notifyOnce(res)
	return false
}

func (u *updater) check(ctx context.Context) (Update, error) {
	platform := u.platformOrDefault()
	if _, _, ok := platform.Bundle(); !ok {
		return Update{}, errUpdateNeedsBundle
	}
	r, err := u.resolve(ctx, platform)
	if err != nil {
		return Update{}, err
	}
	return Update{Available: r.newer, Version: r.release.Version, Notes: clipNotes(r.release.Notes)}, nil
}

func (u *updater) notifyOnce(res Update) {
	u.mu.Lock()
	if u.notified == res.Version || u.notifying {
		u.mu.Unlock()
		return
	}
	u.notifying = true
	u.mu.Unlock()

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

// SignUpdateFeed writes the signed manifest.json and manifest.json.sig for one platform archive.
func SignUpdateFeed(outDir, keyPath, version, notes, platform, archivePath, archiveURL string, validFor time.Duration) error {
	return update.WriteFeed(outDir, keyPath, version, notes, platform, archivePath, archiveURL, validFor)
}

// The lifetime bounds SignUpdateFeed applies.
const (
	DefaultFeedLifetime = update.DefaultFeedLifetime
	MaxFeedLifetime     = update.MaxFeedLifetime
)

// ValidateAppVersion reports whether version is semver major.minor.patch[-pre] the updater can compare.
func ValidateAppVersion(version string) error {
	_, err := update.ParseVersion(version)
	return err
}
