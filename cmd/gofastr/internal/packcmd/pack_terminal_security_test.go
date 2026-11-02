package packcmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// Property: `gofastr pack` with no -o prints the packed spec to the
// terminal, and every string in it comes from the app's source, so the
// output carries no raw terminal-control or invisible rune: C0 controls
// other than the line breaks the YAML itself writes, DEL, the C1 set
// (U+009B is an 8-bit CSI), and the bidi/zero-width format runes. needsQuote
// let any string without a YAML-special character through bare, so
// `const appName = "x\x1b]52;c;…\x07"` in a cloned repo's app.go reached
// the terminal as an OSC 52 clipboard write. Quoted strings already escape
// every non-printable rune; the fix sends such strings down that path. A
// map key cannot be quoted (core/yaml never unquotes keys), so one carrying
// such a rune is refused.
//
// Surfaces: a block scalar value (app name, seed cell), a flow-list item,
// a block-list item, and a map key. Each value must also read back exactly.
func TestPackOutputCarriesNoTerminalControls(t *testing.T) {
	hostile := []string{
		"x\x1b]52;c;cHduZWQ=\x07y", // OSC 52 clipboard write
		"x\x1bcy",                  // ESC c: full terminal reset, no bracket to trip needsQuote
		"x\u009d52;c;cHduZWQ=\ay",  // 8-bit OSC 52 clipboard write
		"x\u009b31my",              // 8-bit CSI as a C1 rune
		"x\u202ey",                 // RIGHT-TO-LEFT OVERRIDE
		"x\u200by",                 // ZERO WIDTH SPACE
		"x\x7fy",                   // DEL
		"x\x00y",                   // NUL
		"x\ry",                     // lone CR rewinds the line
	}
	clean := func(t *testing.T, out string) {
		t.Helper()
		for _, r := range out {
			if r == '\n' {
				continue
			}
			if !unicode.IsPrint(r) {
				t.Fatalf("SECURITY: [pack-terminal-controls] pack output carries raw rune %U:\n%q", r, out)
			}
		}
	}
	for _, v := range hostile {
		t.Run(fmt.Sprintf("value %q", v), func(t *testing.T) {
			out, err := encodeAppSpecYAML(appspec.AppSpec{
				App: appspec.AppSpecApp{Name: v, Module: "example.com/probe"},
				Seed: []appspec.AppSpecSeedEntity{{
					Entity: "notes",
					Rows: []map[string]any{{
						"cell": v,
						"tags": []any{"ok", v},
					}},
				}},
			})
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			clean(t, out)
			node, err := coreyaml.Parse(out)
			if err != nil {
				t.Fatalf("emitted file does not parse: %v\n%s", err, out)
			}
			if got, _ := node.Map["app"].Map["name"].Value.(string); got != v {
				t.Errorf("app name round-tripped to %q, want %q", got, v)
			}
			row := node.Map["seed"].List[0].Map["rows"].List[0]
			if got, _ := row.Map["cell"].Value.(string); got != v {
				t.Errorf("seed cell round-tripped to %q, want %q", got, v)
			}
			if tags := row.Map["tags"]; tags == nil || len(tags.List) != 2 {
				t.Errorf("flow list did not round-trip: %+v", tags)
			} else if got, _ := tags.List[1].Value.(string); got != v {
				t.Errorf("flow-list item round-tripped to %q, want %q", got, v)
			}
		})
	}
	t.Run("block list item", func(t *testing.T) {
		var sb strings.Builder
		if err := writeYAMLListItem(&sb, hostile[0], 0, nil, "x"); err != nil {
			t.Fatal(err)
		}
		clean(t, sb.String())
	})
	// The check's notes name files from the app directory and print to the
	// same terminal (stderr when the YAML is stdout, stdout under -o).
	t.Run("check notes", func(t *testing.T) {
		c := packCheck{
			HandWritten: []string{"entities/" + hostile[1] + ".go"},
			Unread:      []packFileNote{{Path: hostile[2] + ".go", Note: "a symlink"}},
			Unrecovered: []packFileNote{{Path: hostile[4] + ".go", Note: "differs"}},
		}
		clean(t, clitest.CovT_capStdout(t, func() { reportPackCheck(c, false) }))
		errF, err := os.CreateTemp(t.TempDir(), "stderr-*")
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stderr
		os.Stderr = errF
		reportPackCheck(c, true)
		os.Stderr = old
		got, err := os.ReadFile(errF.Name())
		_ = errF.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "entities/x") {
			t.Fatalf("setup broken: the notes never printed: %q", got)
		}
		clean(t, string(got))
	})
	t.Run("map key", func(t *testing.T) {
		for _, k := range hostile {
			if _, err := encodeKeyValue(k, "v"); err == nil {
				t.Errorf("SECURITY: [pack-terminal-controls] key %q was emitted raw; a key cannot be quoted, so it must be refused", k)
			}
		}
	})
}
