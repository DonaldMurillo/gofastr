package releasecmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// includeNameRe is an --include file's name inside the archives.
var includeNameRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

// artifact is one archive the release wrote.
type artifact struct {
	target target
	file   string // base name in the output directory
	sha256 string
}

// result is what a release wrote.
type result struct {
	name      string
	out       string // absolute output directory
	artifacts []artifact
	scripts   []string
	warnings  []string
}

// archiveName is the download name for one build:
// <name>_<version>_<os>_<arch>.tar.gz, or .zip on Windows.
func archiveName(name, version string, t target) string {
	ext := ".tar.gz"
	if t.goos == "windows" {
		ext = ".zip"
	}
	return name + "_" + version + "_" + t.goos + "_" + t.goarch + ext
}

// release builds every target of opts from the module in dir and writes
// the archives, the install scripts and checksums.txt into opts.out.
// Progress lines go to log.
func release(dir string, opts options, log io.Writer) (_ result, err error) {
	info, err := inspectPackage(dir, opts.pkg, opts.targets)
	if err != nil {
		return result{}, err
	}
	res := result{name: opts.name}
	if res.name == "" {
		res.name = defaultName(info.importPath)
		if !nameRe.MatchString(res.name) {
			return result{}, fmt.Errorf("release: the binary would be named %q, which the install scripts cannot use; pass --name", res.name)
		}
	}
	switch {
	case opts.service && !info.usesService:
		return result{}, fmt.Errorf("release: --service runs `%s service install` after the install, but %s does not use battery/service on every target", res.name, info.importPath)
	case !opts.service && info.importsService && opts.baseURL != "":
		res.warnings = append(res.warnings, fmt.Sprintf("%s uses battery/service; pass --service so the install scripts also run `%s service install`", info.importPath, res.name))
	}
	if info.importsDesktop {
		res.warnings = append(res.warnings, info.importPath+" uses battery/desktop; build its window app with `gofastr desktop build`, which signs and bundles it. These archives hold the plain binary")
	}
	windows := slices.ContainsFunc(opts.targets, func(t target) bool { return t.goos == "windows" })
	if windows && !project.ValidWindowsFileName(res.name) {
		return result{}, fmt.Errorf("release: %s.exe is a reserved name on Windows; pass another --name", res.name)
	}
	includes, err := resolveIncludes(dir, opts.includes, res.name, windows)
	if err != nil {
		return result{}, err
	}

	out := opts.out
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	res.out = filepath.Clean(out)
	if err := prepareOut(res.out); err != nil {
		return result{}, err
	}
	r, err := os.OpenRoot(res.out)
	if err != nil {
		return result{}, fmt.Errorf("release: %v", err)
	}
	defer func() { _ = r.Close() }()
	root := &outDir{root: r}
	// On failure, remove the files this run created, so the next run does
	// not trip over a partial release. Only those: another release writing
	// into the same directory keeps its files.
	defer func() {
		if err != nil {
			root.removeCreated()
		}
	}()

	staging, err := os.MkdirTemp("", "gofastr-release-")
	if err != nil {
		return result{}, fmt.Errorf("release: %v", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	for _, t := range opts.targets {
		fmt.Fprintf(log, "  building %s\n", t)
		var a artifact
		a, err = buildTarget(dir, staging, root, opts, res.name, t, includes)
		if err != nil {
			return result{}, err
		}
		res.artifacts = append(res.artifacts, a)
	}
	if opts.baseURL != "" {
		if res.scripts, err = writeScripts(root, scriptData(res, opts)); err != nil {
			return result{}, err
		}
	}
	if err := writeChecksums(root, res); err != nil {
		return result{}, err
	}
	return res, nil
}

// resolveIncludes checks the --include files: regular files (a symlink
// is refused), with base names distinct from each other and from the
// binary.
// Names are compared case-folded, because the archives unpack onto
// case-insensitive file systems on macOS and Windows, where an include
// named HELLO would overwrite the binary hello. A name is held to ASCII
// so the fold is exact, and to the Windows rules when a Windows target
// is built.
func resolveIncludes(dir string, names []string, bin string, windows bool) ([]archiveFile, error) {
	taken := map[string]bool{asciicase.Lower(bin): true, asciicase.Lower(bin + ".exe"): true}
	var files []archiveFile
	for _, n := range names {
		p := n
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return nil, fmt.Errorf("release: --include %s: %v", n, err)
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("release: --include %s must be a regular file, not a symlink or directory", n)
		}
		base := filepath.Base(p)
		if !includeNameRe.MatchString(base) || windows && !project.ValidWindowsFileName(base) {
			return nil, fmt.Errorf("release: --include %s: the archived name %q must be ASCII letters, digits, '.', '_', '+' and '-', and a valid Windows file name when Windows is a target", n, base)
		}
		key := asciicase.Lower(base)
		if taken[key] {
			return nil, fmt.Errorf("release: --include %s: an archive already holds a file named %s, ignoring case", n, base)
		}
		taken[key] = true
		files = append(files, archiveFile{name: base, src: p, mode: 0o644})
	}
	return files, nil
}

// prepareOut creates the output directory, or accepts an existing empty
// one. A directory with files in it is refused, so an old build's
// archives never sit beside a new checksums.txt.
func prepareOut(out string) error {
	//gofastr:allow(worldreadable) release archives and install scripts are public downloads; the directory holds no state or secret
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("release: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return fmt.Errorf("release: %v", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("release: %s is not empty; remove it or pass another -o", out)
	}
	return nil
}

// buildTarget cross-compiles one target into staging and writes its
// archive into root.
func buildTarget(dir, staging string, root *outDir, opts options, name string, t target, includes []archiveFile) (artifact, error) {
	bin := name
	if t.goos == "windows" {
		bin += ".exe"
	}
	binPath := filepath.Join(staging, t.goos+"_"+t.goarch, bin)
	ldflags := "-s -w -X " + opts.versionVar + "=" + opts.version
	if opts.ldflags != "" {
		ldflags += " " + opts.ldflags
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags="+ldflags, "-o", binPath, opts.pkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), buildEnv(t)...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return artifact{}, fmt.Errorf("release: go build for %s: %v", t, err)
	}

	files := append([]archiveFile{{name: bin, src: binPath, mode: 0o755}}, includes...)
	a := artifact{target: t, file: archiveName(name, opts.version, t)}
	//gofastr:allow(worldreadable) a release archive is a public download, holds no state or secret
	f, err := root.create(a.file, 0o644)
	if err != nil {
		return artifact{}, fmt.Errorf("release: %v", err)
	}
	h := sha256.New()
	w := io.MultiWriter(f, h)
	if t.goos == "windows" {
		err = writeZip(w, files)
	} else {
		err = writeTarGz(w, files)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return artifact{}, fmt.Errorf("release: writing %s: %v", a.file, err)
	}
	a.sha256 = hex.EncodeToString(h.Sum(nil))
	return a, nil
}

// writeChecksums writes checksums.txt in the `sha256sum` format, one
// line per archive and script, sorted by name, so `sha256sum -c
// checksums.txt` checks a downloaded set.
func writeChecksums(root *outDir, res result) error {
	lines := make([]string, 0, len(res.artifacts)+len(res.scripts))
	for _, a := range res.artifacts {
		lines = append(lines, a.sha256+"  "+a.file)
	}
	for _, s := range res.scripts {
		sum, err := fileSHA256(root, s)
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+s)
	}
	slices.SortFunc(lines, func(a, b string) int { return strings.Compare(a[66:], b[66:]) })
	return writeNew(root, "checksums.txt", []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func fileSHA256(root *outDir, name string) (string, error) {
	data, err := root.root.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("release: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// writeNew creates name under root; an existing file is an error.
func writeNew(root *outDir, name string, data []byte, mode fs.FileMode) error {
	f, err := root.create(name, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("release: writing %s: %v", name, err)
	}
	return nil
}

// outDir is the output directory, recording the files this run created.
type outDir struct {
	root    *os.Root
	created []string
}

// create makes name under the directory; an existing file is an error,
// so a file this run did not create is never recorded as its own.
func (o *outDir) create(name string, mode fs.FileMode) (*os.File, error) {
	f, err := o.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return nil, fmt.Errorf("release: %v", err)
	}
	o.created = append(o.created, name)
	return f, nil
}

// removeCreated deletes the files this run created.
func (o *outDir) removeCreated() {
	for _, name := range o.created {
		_ = o.root.Remove(name)
	}
}
