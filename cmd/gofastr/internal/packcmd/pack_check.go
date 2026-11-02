package packcmd

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// packCheck is what pack reports beside the spec it writes: the Go files it
// left out as hand-written, the files the packed spec generates
// differently from what is on disk (or that are not on disk), and the
// files it could not read, which pack left out of the spec too.
type packCheck struct {
	HandWritten []string
	Unrecovered []packFileNote
	Unread      []packFileNote
}

// packFileNote is one file the packed spec does not reproduce, and why.
type packFileNote struct {
	Path string
	Note string
}

// checkPack regenerates the app from the packed spec and compares each Go
// file pack's layout covers (the app root and entities/) with the file on
// disk, provenance markers aside. A file the spec reproduces byte for byte
// was recovered. The marker says why one was not: edited since it was
// generated, untouched and still different (pack lost part of it), or
// unmarked (written before markers, or by hand). An unmarked file the spec
// does not generate at all is hand-written and outside the spec, and so is
// a file an endpoint links: the spec names the handler, the body is the
// developer's. Files
// another generator marked are not the app generator's to recover. Line
// endings and a byte order mark are not differences: a Windows checkout
// holds the same code.
func checkPack(dir string, bp appspec.AppSpec) (packCheck, error) {
	var c packCheck
	rendered, err := appgen.RenderAppSpecCode(bp)
	if err == nil {
		rendered, err = appgen.StampGeneratedFiles("app", rendered)
	}
	if err != nil {
		return c, fmt.Errorf("regenerate from the packed spec: %w", err)
	}
	want := map[string]string{}
	handlers := endpointHandlerFiles(bp)
	for _, f := range rendered {
		// The renderer joins with the OS separator; the check walks the
		// app with slash paths.
		name := filepath.ToSlash(f.Name)
		if !packCheckScope(name) || !slices.Contains(packCheckDirs, path.Dir(name)) || handlers[name] {
			continue
		}
		want[name] = codegen.StripMarker(name, f.Content)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return c, err
	}
	defer root.Close()
	files, skipped := packCheckFiles(root)
	seen := map[string]bool{}
	for _, n := range skipped {
		seen[n.Path] = true
		c.Unread = append(c.Unread, n)
	}
	for _, rel := range files {
		seen[rel] = true
		if handlers[rel] {
			// The spec links the handler; its body is the developer's.
			c.HandWritten = append(c.HandWritten, rel)
			continue
		}
		_, generated := want[rel]
		content, err := readPackCheckFile(root, rel, generated)
		if err != nil {
			c.Unread = append(c.Unread, packFileNote{Path: rel, Note: err.Error()})
			continue
		}
		m, body, marked := codegen.ReadMarker(rel, content)
		if !marked {
			body = content
		} else if m.Generator != "app" {
			continue
		}
		if !generated && !marked {
			c.HandWritten = append(c.HandWritten, rel)
			continue
		}
		edited := marked && codegen.Sum(body) != m.Sum
		var note string
		switch wantBody := want[rel]; {
		case !generated:
			note = "generated, but the packed spec no longer produces it"
			if edited {
				note += "; edited since it was generated"
			}
		case wantBody == body:
			continue
		case edited:
			note = "edited since it was generated, and differs from what the packed spec generates"
		case marked:
			note = untouchedNote(m.Version)
		default:
			note = "has no gofastr marker and differs from what the packed spec generates"
		}
		c.Unrecovered = append(c.Unrecovered, packFileNote{Path: rel, Note: withSection(rel, note)})
	}
	for _, rel := range slices.Sorted(maps.Keys(want)) {
		if !seen[rel] {
			c.Unrecovered = append(c.Unrecovered, packFileNote{Path: rel, Note: withSection(rel, "the packed spec generates it, but it is not in the app")})
		}
	}
	return c, nil
}

// untouchedNote says why an untouched marked file differs from what the
// packed spec generates. Pack lost part of it when this release wrote the
// file; when another release wrote it, the template may have changed
// instead. A dev build cannot tell, so the note says either may hold.
func untouchedNote(version string) string {
	current := appgen.MarkerVersion(project.CliVersion())
	switch {
	case version == current && project.IsReleaseTag(current):
		return "untouched since it was generated, yet the packed spec generates it differently: pack lost part of it"
	case project.IsReleaseTag(version):
		return "untouched since gofastr " + version + " generated it; this gofastr (" + current + ") generates it differently from the packed spec: the template changed, or pack lost part of it"
	}
	return "untouched since it was generated, yet the packed spec generates it differently: pack lost part of it, or the template changed since"
}

func withSection(rel, note string) string {
	if section := packSection(rel); section != "" {
		return note + " (" + section + ")"
	}
	return note
}

// packCheckHeader is how much of a file the check reads to find a marker
// before it knows it needs the rest: more than the marker's header lines
// of any generated file.
const packCheckHeader = 64 << 10

// readPackCheckFile reads rel for the check with line endings and a byte
// order mark normalized. A file the spec does not generate is read in
// full only when its header carries a marker: an unmarked one is
// hand-written whatever follows, so its first bytes are enough.
func readPackCheckFile(root *os.Root, rel string, generated bool) (string, error) {
	f, err := root.Open(rel)
	if err != nil {
		return "", packReadErr(err)
	}
	defer f.Close()
	head, err := io.ReadAll(io.LimitReader(f, packCheckHeader))
	if err != nil {
		return "", packReadErr(err)
	}
	if !generated {
		if _, _, marked := codegen.ReadMarker(rel, normalizePackSource(string(head))); !marked {
			return normalizePackSource(string(head)), nil
		}
	}
	rest, err := io.ReadAll(f)
	if err != nil {
		return "", packReadErr(err)
	}
	return normalizePackSource(string(head) + string(rest)), nil
}

func packReadErr(err error) error {
	if os.IsPermission(err) {
		return fmt.Errorf("could not read it: permission denied")
	}
	return fmt.Errorf("could not read it: %v", err)
}

// normalizePackSource drops a UTF-8 byte order mark and turns CRLF line
// endings into LF.
func normalizePackSource(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// packCheckScope reports whether the check compares rel: Go source, not a
// test. Tests are not part of the spec.
func packCheckScope(rel string) bool {
	return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
}

// packCheckDirs are the directories pack reads Go files from.
var packCheckDirs = []string{".", "entities"}

// packCheckFiles lists the files the check compares, sorted: those in
// packCheckScope at the app root and directly under entities/, the layout
// pack reads. An entry in scope that is not a regular file (a symlink)
// is skipped, as pack skips it, and returned with why.
func packCheckFiles(root *os.Root) (files []string, skipped []packFileNote) {
	for _, d := range packCheckDirs {
		entries, err := appgen.RootReadDirEntries(root, d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			rel := path.Join(d, e.Name())
			if !packCheckScope(rel) || e.IsDir() {
				continue
			}
			if e.Type().IsRegular() {
				files = append(files, rel)
				continue
			}
			note := "not a regular file, so pack does not read it"
			if e.Type()&os.ModeSymlink != 0 {
				note = "a symlink, so pack does not read it"
			}
			skipped = append(skipped, packFileNote{Path: rel, Note: note})
		}
	}
	slices.Sort(files)
	slices.SortFunc(skipped, func(a, b packFileNote) int { return strings.Compare(a.Path, b.Path) })
	return files, skipped
}

// packSection names the part of the spec a generated file carries, so a
// note says what the packed spec may be missing.
func packSection(rel string) string {
	base := path.Base(rel)
	switch {
	case strings.HasPrefix(rel, "entities/"):
		return "entities"
	case strings.HasPrefix(base, "screen"):
		return "screens"
	case base == "app.go":
		return "app settings, theme and nav"
	case base == "main.go":
		return "app settings"
	case base == "stubs.go":
		return "seed data and stubs"
	}
	return ""
}

// reportPackCheck prints the check through packNote.
func reportPackCheck(c packCheck, toStderr bool) {
	if len(c.HandWritten) > 0 {
		packNote(toStderr, "pack: %d hand-written file(s) are not in the spec: %s", len(c.HandWritten), strings.Join(c.HandWritten, ", "))
	}
	if len(c.Unread) > 0 {
		packNote(toStderr, "pack: %d file(s) were left out of the spec unread:", len(c.Unread))
		for _, n := range c.Unread {
			packNote(toStderr, "    %s: %s", n.Path, n.Note)
		}
	}
	if len(c.Unrecovered) > 0 {
		packNote(toStderr, "pack: %d file(s) generate differently from the packed spec:", len(c.Unrecovered))
		for _, n := range c.Unrecovered {
			packNote(toStderr, "    %s: %s", n.Path, n.Note)
		}
	}
}

// packNote is a pack warning. With no -o the YAML is stdout and
// `pack > gofastr.yml` is the documented pipe, so the warning goes to
// stderr: a line ahead of the document is not valid YAML.
// The stderr spellings carry no color: yellow and red key on stdout's
// terminal, not stderr's.
//
// The message names files and values from the app directory, so it is
// scrubbed (packMessage) before it reaches the terminal.
func packNote(toStderr bool, format string, args ...any) {
	msg := packMessage(format, args...)
	if toStderr {
		fmt.Fprintf(os.Stderr, "  ⚠ %s\n", msg)
		return
	}
	cli.Warn("%s", msg)
}

// packFail reports a pack error and exits 1.
func packFail(toStderr bool, format string, args ...any) {
	msg := packMessage(format, args...)
	if toStderr {
		fmt.Fprintf(os.Stderr, "  ✗ %s\n", msg)
	} else {
		cli.Fail("%s", msg)
	}
	cli.OsExit(1)
}

// packMessage formats a pack note with every terminal-control and
// invisible rune percent-encoded (textsafe.ScrubControlBytes): a file
// name or source string carrying an ESC, an 8-bit CSI/OSC or a bidi
// override would otherwise drive the terminal the note prints to.
func packMessage(format string, args ...any) string {
	return textsafe.ScrubControlBytes(fmt.Sprintf(format, args...))
}
