package main

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// packCheck is what pack reports beside the spec it writes: the Go files it
// left out as hand-written, and the files the packed spec generates
// differently from what is on disk.
type packCheck struct {
	HandWritten []string
	Unrecovered []packFileNote
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
// does not generate at all is hand-written and outside the spec. Files
// another generator marked are not the app generator's to recover.
func checkPack(dir string, bp AppSpec) (packCheck, error) {
	var c packCheck
	rendered, err := renderAppSpecCode(bp)
	if err == nil {
		rendered, err = stampGeneratedFiles("app", rendered)
	}
	if err != nil {
		return c, fmt.Errorf("regenerate from the packed spec: %w", err)
	}
	want := map[string]string{}
	for _, f := range rendered {
		if !packCheckScope(f.name) {
			continue
		}
		if _, body, ok := codegen.ReadMarker(f.name, f.content); ok {
			want[f.name] = body
		} else {
			want[f.name] = f.content
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return c, err
	}
	defer root.Close()
	for _, rel := range packCheckFiles(root) {
		data, err := root.ReadFile(rel)
		if err != nil {
			continue // unreadable leaf (e.g. symlink outside the root): pack skipped it too
		}
		m, body, marked := codegen.ReadMarker(rel, string(data))
		if !marked {
			body = string(data)
		} else if m.Generator != "app" {
			continue
		}
		wantBody, generated := want[rel]
		var note string
		switch {
		case !generated && !marked:
			c.HandWritten = append(c.HandWritten, rel)
			continue
		case !generated:
			note = "generated, but the packed spec no longer produces it"
		case wantBody == body:
			continue
		case marked && codegen.Sum(body) != m.Sum:
			note = "edited since it was generated, and the spec does not capture the edit"
		case marked:
			note = "untouched since it was generated, yet the packed spec generates it differently: pack lost part of it"
		default:
			note = "has no gofastr marker and differs from what the packed spec generates"
		}
		if section := packSection(rel); section != "" {
			note += " (" + section + ")"
		}
		c.Unrecovered = append(c.Unrecovered, packFileNote{Path: rel, Note: note})
	}
	return c, nil
}

// packCheckScope reports whether the check compares rel: Go source, not a
// test. Tests are not part of the spec.
func packCheckScope(rel string) bool {
	return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
}

// packCheckFiles lists the files the check compares, sorted: those in
// packCheckScope at the app root and directly under entities/, the layout
// pack reads.
func packCheckFiles(root *os.Root) []string {
	var out []string
	for _, d := range []string{".", "entities"} {
		entries, err := rootReadDirEntries(root, d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			rel := path.Join(d, e.Name())
			if e.Type().IsRegular() && packCheckScope(rel) {
				out = append(out, rel)
			}
		}
	}
	slices.Sort(out)
	return out
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
func packNote(toStderr bool, format string, args ...any) {
	if toStderr {
		fmt.Fprintf(os.Stderr, "  %s %s\n", yellow("⚠"), fmt.Sprintf(format, args...))
		return
	}
	warn(format, args...)
}
