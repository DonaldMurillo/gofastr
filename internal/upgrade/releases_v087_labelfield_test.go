package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// labelFieldKit is the stub kit side of the LabelForField removal: the
// old helper (gone) and the FieldLabel that replaced it, with its real
// signature — the guidance's job is to move a caller from the first to
// a compiling call of the second.
var labelFieldKit = map[string]string{
	"framework/i18nui/i18nui.go": `package i18nui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func LabelForField(ctx context.Context, tr *i18n.Translator, entity, field string) string {
	return field
}

func FieldLabel(ctx context.Context, tr *i18n.Translator, entity, field, display string) string {
	if display != "" {
		return display
	}
	return field
}
`,
}

// The LabelForField note, through the shipped YAML: a call of the
// removed helper is an edit hit, and an app already calling FieldLabel
// with the arguments the guidance tells the mover to use (a label
// string for display) is silent — the positive fixture is what the
// guidance compiles to, so the note cannot fire on its own fix.
func TestV087FieldLabelNoteHitsOldHelper(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	n := scantest.Only(labelFieldNote(t, reg), "uses")

	old := scantest.App(t, map[string]string{"labels.go": `package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func label(ctx context.Context) string {
	return i18nui.LabelForField(ctx, nil, "invoices", "memo")
}
`}, scantest.Options{Kit: labelFieldKit})
	res := scantest.Run(t, old, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if !res.TypeChecked {
		t.Fatalf("old-shape app did not type-check: broken=%v unexplained=%v", res.Broken, res.Unexplained)
	}
	got := scantest.Hits(res, n)
	if len(got) != 1 || !strings.Contains(got[0], "LabelForField") {
		t.Fatalf("hits = %v, want the one LabelForField call", got)
	}

	fixed := scantest.App(t, map[string]string{"labels.go": `package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func label(ctx context.Context) string {
	return i18nui.FieldLabel(ctx, nil, "invoices", "memo", "")
}
`}, scantest.Options{Kit: labelFieldKit})
	res = scantest.Run(t, fixed, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if !res.TypeChecked {
		t.Fatalf("fixed app did not type-check: broken=%v unexplained=%v", res.Broken, res.Unexplained)
	}
	if got := scantest.Hits(res, n); len(got) != 0 {
		t.Fatalf("fires on the migrated FieldLabel call: %v", got)
	}
}

// labelFieldNote finds the note by its change text, so inserting notes
// above it cannot silently move the index this test reads.
func labelFieldNote(t *testing.T, reg *upgrade.Registry) *upgrade.Note {
	t.Helper()
	for _, r := range reg.Releases {
		if r.Version != "v0.87.0" {
			continue
		}
		for i := range r.Notes {
			if strings.Contains(r.Notes[i].Change, "LabelForField is removed") {
				return r.Notes[i]
			}
		}
	}
	t.Fatal("v0.87.0 LabelForField note not found")
	return nil
}
