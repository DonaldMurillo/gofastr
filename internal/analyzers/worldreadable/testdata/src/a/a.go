// Package a holds the worldreadable fixture reduced from the real
// pre-fix sites: framework/export_data.go (ExportData MkdirAll 0755 +
// manifest.json/writeNDJSON 0644), harness session export.go (os.Create
// on the tmp zip), session/sqlite retention.go (OpenCostLedger MkdirAll
// 0755), isolation.go (sqliteDSN MkdirAll 0755), kiln/journal
// journal.go (OpenJSONL OpenFile 0644 + the TruncateAfter pair + its
// parent MkdirAll 0755), each with its fixed spelling or the matching
// silent posture next to it.
package a

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- pre-fix: App.ExportData (framework/export_data.go) ----------

// ExportData reduces ExportData: the dump dir is created 0755 and the
// manifest + ndjson files land 0644, so every local co-user of the host
// reads the raw credential-bearing dump.
func ExportData(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil { // want `directory created with mode 0o755 under dir`
		return err
	}
	for _, src := range []string{"users", "sessions"} {
		rows := []map[string]any{{"password_hash": "x"}}
		if err := writeNDJSON(filepath.Join(dir, src+".ndjson"), rows); err != nil {
			return err
		}
	}
	mb, _ := json.MarshalIndent(map[string]int{"v": 1}, "", "  ")
	return os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644) // want `file created with mode 0o644 under filepath.Join\(dir, "manifest.json"\)`
}

// writeNDJSON reduces export_data.go:573: a state dump written at a
// caller-chosen path with a constant 0644.
func writeNDJSON(path string, rows []map[string]any) error {
	return os.WriteFile(path, []byte("row"), 0o644) // want `file created with mode 0o644 under path`
}

// exportDataFixed is the fix posture: owner-only dir and files (the
// import side only reads).
func ExportDataFixed(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mb, _ := json.MarshalIndent(map[string]int{"v": 1}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o600); err != nil {
		return err
	}
	return writeNDJSONFixed(filepath.Join(dir, "users.ndjson"))
}

func writeNDJSONFixed(path string) error {
	return os.WriteFile(path, []byte("row"), 0o600)
}

// ---------- pre-fix: ExportBundle.Write (harness session export.go) ----------

type exportBundle struct {
	OutPath string
}

// writeBundle reduces session/export.go:55: os.Create on the tmp zip is
// umask-default 0666 (0644 on a typical umask), while the sibling
// session store is owner-only.
func (b *exportBundle) writeBundle() error {
	tmp := b.OutPath + ".tmp"
	f, err := os.Create(tmp) // want `file created with mode 0o666 \(os\.Create, umask default\) under tmp`
	if err != nil {
		return err
	}
	defer f.Close()
	return os.Rename(tmp, b.OutPath)
}

func (b *exportBundle) writeBundleFixed() error {
	tmp := b.OutPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return os.Rename(tmp, b.OutPath)
}

// ---------- pre-fix: OpenCostLedger (session/sqlite retention.go) ----------

// openCostLedger reduces retention.go:157: the ledger dir is created
// 0755 and nothing else in the function names its content, so the dir
// the spend data lands in stays group/world-traversable.
func OpenCostLedger(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // want `directory created with mode 0o755 under filepath.Dir\(path\)`
		return err
	}
	return nil
}

func OpenCostLedgerFixed(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return nil
}

// ---------- pre-fix: Runtime.sqliteDSN (isolation.go) ----------

func SQLiteDSN(projectDir, id string) (string, error) {
	dir := filepath.Join(projectDir, ".gofastr", "isolation", id)
	if err := os.MkdirAll(dir, 0o755); err != nil { // want `directory created with mode 0o755 under dir`
		return "", err
	}
	return filepath.Join(dir, "app.db"), nil
}

func SQLiteDSNFixed(projectDir, id string) (string, error) {
	dir := filepath.Join(projectDir, ".gofastr", "isolation", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "app.db"), nil
}

// ---------- pre-fix: OpenJSONL + TruncateAfter (kiln/journal journal.go) ----------

// openJournal reduces journal.go: the journal dir is 0755, the journal
// file itself 0644, and it holds SetAppConfigPayload entries with
// JWTSecret and SeedPassword verbatim while freeze writes the same data
// 0600 + fileperm.Restrict.
func OpenJournal(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // want `directory created with mode 0o755 under filepath.Dir\(path\)`
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644) // want `file created with mode 0o644 under path`
	if err != nil {
		return err
	}
	return f.Close()
}

// truncateJournal reduces the TruncateAfter pair: the tmp rewrite and
// the reopen both stay 0644.
func TruncateJournal(path string) error {
	tmpPath := path + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644) // want `file created with mode 0o644 under tmpPath`
	if err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0o644) // want `file created with mode 0o644 under path`
	if err != nil {
		return err
	}
	return f.Close()
}

func OpenJournalFixed(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// ---------- silent posture: non-constant mode (caller owns the policy) ----------

type logOpts struct {
	FileMode os.FileMode
}

// logSink reduces battery/log file.go: the mode is the caller's field.
func LogSink(path string, opts logOpts) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, opts.FileMode)
	if err != nil {
		return err
	}
	return f.Close()
}

func ModeFromCaller(path string, mode os.FileMode) error {
	return os.WriteFile(path, []byte("x"), mode)
}

// ---------- silent posture: throwaway roots ----------

// stageInTmp writes a 0644 file under an os.MkdirTemp root: throwaway.
func StageInTmp(rel string) error {
	tmp, err := os.MkdirTemp("", "export-*")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(tmp, rel), []byte("x"), 0o644)
}

// stageInTestDir writes under t.TempDir.
func StageInTestDir(t *testing.T, rel string) error {
	return os.WriteFile(filepath.Join(t.TempDir(), rel), []byte("x"), 0o644)
}

// ---------- silent posture: read-first (perm applies only at creation) ----------

// editExisting reduces the harness Edit builtin: the file was read, so
// the 0644 perm argument cannot change an existing file's mode.
func EditExisting(path, old, replacement string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := replaceOne(string(data), old, replacement)
	return os.WriteFile(path, []byte(updated), 0o644)
}

func replaceOne(s, old, new string) string {
	i := strings.Index(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

// memoryNotes reduces harness memory.go: agent memory entries are .md.
func MemoryNotes(root string) error {
	return os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("notes\n"), 0o644)
}

// lockFile reduces cmd/mutate main.go: a lock file carries no payload.
func LockFile(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, ".mutate.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// ---------- silent posture: public by output/build-root provenance ----------

type staticConfig struct {
	OutDir string
}

func StaticBuild(s staticConfig) error {
	return os.MkdirAll(s.OutDir, 0o755)
}

// copyAssets reduces framework/static builder.go: writes under a dst.
func CopyAssets(src, dst string) error {
	rel := "app.css"
	out := filepath.Join(dst, rel)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = w.Write([]byte("body{}"))
	return err
}

// ---------- silent posture: codegen output root through a helper hop ----------

type genOptions struct {
	OutputRoot string
}

type genFile struct {
	Path    string
	Content string
	Mode    os.FileMode
}

// genManifestName reduces codegen ManifestName (.codegen-manifest.json).
const genManifestName = ".gen-manifest.json"

func safeRoot(root string) string {
	return filepath.Clean(root)
}

// writeGenManifest reduces codegen fileset.go writeManifest: the root is
// the caller's OUTPUT root (generator output, committed next to the
// generated source).
func writeGenManifest(root string) error {
	return os.WriteFile(filepath.Join(root, genManifestName), []byte("{}\n"), 0o644)
}

// writeGeneratedFile reduces codegen fileset.go: the default-mode branch
// writes generated source under the output root.
func writeGeneratedFile(path string, file genFile) error {
	if file.Mode == 0 {
		return os.WriteFile(path, []byte(file.Content), 0o644)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(file.Content)
	return err
}

func GenerateInto(opts genOptions) error {
	root := safeRoot(opts.OutputRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := writeGeneratedFile(filepath.Join(root, "main.go"), genFile{Content: "package main\n"}); err != nil {
		return err
	}
	return writeGenManifest(root)
}

// ---------- silent posture: directory whose reachable content is public ----------

// freeze reduces kiln/freeze freeze.go: the blueprint dir is 0755 but
// everything written into it (through the helpers it passes dir to) is
// either committed generated YAML or owner-only.
func Freeze(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeBlueprint(dir); err != nil {
		return err
	}
	return writeWorldSnapshot(dir)
}

func writeBlueprint(dir string) error {
	return os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte("app: {}\n"), 0o644)
}

func writeWorldSnapshot(dir string) error {
	return os.WriteFile(filepath.Join(dir, "world.json"), []byte("{}\n"), 0o600)
}

type migrationOpts struct {
	MigrationsDir string
}

// generateMigration reduces framework/migrate generate_file.go: the
// migrations dir holds committed .sql files.
func GenerateMigration(opts migrationOpts, name string) error {
	if err := os.MkdirAll(opts.MigrationsDir, 0o755); err != nil {
		return err
	}
	filename := fmt.Sprintf("%04d_%s.sql", 1, name)
	return os.WriteFile(filepath.Join(opts.MigrationsDir, filename), []byte("-- up\n"), 0o644)
}

// writeAgentDocs reduces cmd/gofastr agents.go: the agents/ dir's only
// writes target files the function already read.
func WriteAgentDocs(dir string) error {
	target := filepath.Join(dir, "agents")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"agents/battery-admin.md"} {
		path := filepath.Join(dir, name)
		if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
			continue
		}
		if err := os.WriteFile(path, []byte("doc\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ---------- silent posture: committed-record writer ----------

// writeBaseline reduces framework/contracts baseline.go: the baseline is
// a reviewed, committed record by its own documentation.
func WriteBaseline(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte("{}\n"), 0o644)
}

// ---------- silent posture: test scaffolding ----------

// writeFileScaffold reduces cmd/gofastr audit.go's mkdirAll/writeFile
// wrappers: unexported, and no non-test code calls them.
func writeFileScaffold(p string, b []byte) error {
	return os.WriteFile(p, b, 0o644)
}

// ---------- silent posture: executable files ----------

// writeShims reduces evals ui-quality clishim.go: a 0755 FILE is an
// executable; the group bits are the deployment posture.
func WriteShims(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "gofastr"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "gofastr.cmd"), []byte("@echo off\n"), 0o755)
}

// ---------- silent posture: VCS scaffold dotfiles ----------

// scaffoldProject reduces cmd/gofastr init.go: committed project files.
func ScaffoldProject(name string) error {
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte("gen/\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(name, "static", ".gitkeep"), nil, 0o644)
}

// ---------- silent posture: caller read the file first ----------

// Restore reduces cmd/mutate main.go's restore: the caller read the
// source before mutating, so the write targets a known-existing file.
func MutateThenRestore(path string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("mutant"), 0o644); err != nil {
		return err
	}
	return restoreOriginal(path, original)
}

func restoreOriginal(path string, original []byte) error {
	return os.WriteFile(path, original, 0o644)
}

// ---------- silent posture: dir serving a caller-owned mode ----------

// writeToolReduce reduces harness tool/builtins write.go: the parent
// dir is created for a write whose MODE the caller supplies.
func WriteToolReduce(argsPath string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(argsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(argsPath, []byte("x"), mode)
}

// ---------- silent posture: CreateTemp atomic-write parent ----------

// writeThemeBack reduces cmd/gofastr theme_edit.go: the dir receives a
// CreateTemp mint whose mode the write sets explicitly on the handle.
func WriteThemeBack(outPath string) error {
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".theme.go.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write([]byte("package theme\n")); err != nil {
		tmp.Close()
		return err
	}
	return os.Rename(tmp.Name(), outPath)
}
