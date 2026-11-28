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
	"github.com/DonaldMurillo/gofastr/internal/bom"
)

type packCheck struct {
	HandWritten []string
	Unrecovered []packFileNote
	Unread      []packFileNote
}

type packFileNote struct {
	Path string
	Note string
}

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
		name := filepath.ToSlash(f.Name)
		if !appgen.IsProductionGoSource(name) || !slices.Contains(packCheckDirs, path.Dir(name)) || handlers[name] {
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

const packCheckHeader = 64 << 10

func readPackCheckFile(root *os.Root, rel string, generated bool) (string, error) {
	f, err := appgen.OpenRegularSource(root, rel)
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

func normalizePackSource(s string) string {
	return strings.ReplaceAll(bom.Trim(s), "\r\n", "\n")
}

var packCheckDirs = []string{".", "entities"}

func packCheckFiles(root *os.Root) (files []string, skipped []packFileNote) {
	for _, d := range packCheckDirs {
		entries, err := appgen.RootReadDirEntries(root, d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			rel := path.Join(d, e.Name())
			if !appgen.IsProductionGoSource(rel) || e.IsDir() {
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

func packNote(toStderr bool, format string, args ...any) {
	msg := packMessage(format, args...)
	if toStderr {
		cli.Fprintf(os.Stderr, "  ⚠ %s\n", msg)
		return
	}
	cli.Warn("%s", msg)
}

func packFail(toStderr bool, format string, args ...any) {
	msg := packMessage(format, args...)
	if toStderr {
		cli.Fprintf(os.Stderr, "  ✗ %s\n", msg)
	} else {
		cli.Fail("%s", msg)
	}
	cli.OsExit(1)
}

func packMessage(format string, args ...any) string {
	return textsafe.ScrubControlBytes(fmt.Sprintf(format, args...))
}
