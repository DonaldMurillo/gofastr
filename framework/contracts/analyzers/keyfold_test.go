package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1410 exists because battery/storage's local backend refuses a
// folded key before it writes (refuseFoldedKey) while core/upload's
// LocalStorage.Save does not (probe TestFoldedKeysDoNotAlias,
// 2026-09-05 red round): on a case-insensitive filesystem
// `TenantA/report.txt` and `tenanta/report.txt` are one file, so each
// tenant's save silently overwrites the other's object. The fixtures
// reduce both backends to the shape under different identifiers and
// pin every silent posture.

// The repo site, reduced: a local backend with a root field whose Save
// renames into place with no fold refusal anywhere in the file.
func TestUnguardedLocalSaveIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"vault.go": `package vault

import "os"

type boxStore struct {
	BaseDir string
}

func (s *boxStore) Save(key string, payload []byte) error {
	tmp, err := os.CreateTemp(s.BaseDir, ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	return os.Rename(tmp.Name(), s.BaseDir+"/"+key)
}
`})
	d := assertHas(t, ds, contracts.RuleFoldedKey)
	if d.Line != 19 {
		t.Errorf("finding on line %d, want 19 (the rename, the write the fold check must precede): %s", d.Line, d.Location())
	}
}

// The fix posture: the same Save with the fold refusal in the same
// file — battery/storage's spelling, wherever in the file it lives.
func TestFoldRefusalIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"vault.go": `package vault

import "os"

type boxStore struct {
	BaseDir string
}

func (s *boxStore) Save(key string, payload []byte) error {
	if err := s.refuseFoldedKey(key); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.BaseDir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp.Write(payload)
	tmp.Close()
	return os.Rename(tmp.Name(), s.BaseDir+"/"+key)
}

func (s *boxStore) refuseFoldedKey(key string) error { return nil }
`,
	})
	assertNot(t, ds, contracts.RuleFoldedKey, "refuseFoldedKey in the same file is the documented fix posture")
}

// A positive with no counterpart in this repo: different identifiers,
// a *os.Root root field, a Put spelling, os.Create as the sink.
func TestFoldedKeyFiresOnUnrelatedSites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"crate.go": `package crate

import "os"

type shelf struct {
	root *os.Root
}

func (sh *shelf) Put(name string, payload []byte) error {
	f, err := os.Create("/nonexistent/" + name)
	if err != nil {
		return err
	}
	f.Write(payload)
	return f.Close()
}
`,
	})
	d := assertHas(t, ds, contracts.RuleFoldedKey)
	if d.Line != 10 {
		t.Errorf("finding on line %d, want 10 (os.Create, the first disk write in Put): %s", d.Line, d.Location())
	}
}

// Silent postures, each pinned: no root field on the type, a Save that
// never reaches a disk write, and a root field of a non-filesystem
// type.
func TestFoldedKeySilentPosturesAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"quiet.go": `package quiet

import (
	"database/sql"
	"os"
)

// No BaseDir/Root/Dir field: a database backend has no on-disk
// spelling to fold.
type rowKeeper struct {
	dsn string
}

func (r *rowKeeper) Save(key string, payload []byte) error {
	_, err := sql.Open("sqlite", r.dsn)
	return err
}

// A root field, but the Save never reaches os.OpenFile/os.Create/
// os.Rename: nothing lands on disk from this method.
type memCache struct {
	dir string
}

func (m *memCache) Save(key string, payload []byte) error {
	_ = os.Getenv("HOME")
	return m.stash(key, payload)
}

func (m *memCache) stash(string, []byte) error { return nil }

// A field named dir but typed int: not a filesystem root.
type counter struct {
	dir int
}

func (c *counter) Save(key string, payload []byte) error {
	_, err := os.Create("/tmp/fixed")
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleFoldedKey, "no local-filesystem root reaches a disk write in any of these")
}

// A writer that computes its own file name — the harness
// DailyFileWriter shape: no key parameter, the name is a formatted
// date — has no caller key to fold.
func TestSelfNamedWriterIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"daily.go": `package daily

import (
	"os"
	"path/filepath"
	"time"
)

type dailyWriter struct {
	dir string
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	today := time.Now().UTC().Format("20060102")
	path := filepath.Join(w.dir, "harness-"+today+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Write(p)
}
`,
	})
	assertNot(t, ds, contracts.RuleFoldedKey, "no key parameter: the file name is self-computed, nothing caller-named can fold")
}
