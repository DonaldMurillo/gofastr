package entityui

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/fieldtext"
)

func sweepCaptureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func sweepKindUI(t *testing.T, k Kind, mutate func(*Extensions)) *testUI {
	t.Helper()
	cfg := dealsConfig()
	cfg.Display.Fields["name"] = entity.FieldDisplay{Input: "probe"}
	ext := Extensions{Kinds: map[string]Kind{"probe": k}}
	x := newTestUIExt(t, map[string]entity.EntityConfig{"deals": cfg},
		map[string][]map[string]any{"deals": {{"id": "d1", "name": "alpha", "amount": "1", "fee": "0"}}},
		ext, withAPI(map[string]string{"deals": "/api/deals"}))
	if mutate != nil {
		mutate(&ext)
	}
	return x
}

// A panicking kind Cell fails its slot with the generic message, and the log names only the panic's type.
func TestSweepCoreSlotPanicLogsTypeOnly(t *testing.T) {
	buf := sweepCaptureLog(t)
	x := sweepKindUI(t, Kind{Cell: func(CellContext) render.HTML { panic("secret-cell-text") }}, nil)
	body := string(x.ui.List("deals").Base("/deals").Columns("name", "amount").RenderCtx(x.userCtx("/deals", "", "u1")))
	if strings.Contains(body, "secret-cell-text") || strings.Contains(buf.String(), "secret-cell-text") {
		t.Fatalf("panic text leaked:\nbody: %s\nlog: %s", body, buf.String())
	}
	if !strings.Contains(buf.String(), "slot panicked") {
		t.Fatalf("no slot panic logged: %s", buf.String())
	}
}

// A panicking kind Text leaves its text slot empty, so the row link falls back to the id.
func TestSweepCoreKindTextPanicIsEmpty(t *testing.T) {
	buf := sweepCaptureLog(t)
	x := sweepKindUI(t, Kind{
		Cell: func(cc CellContext) render.HTML { return render.HTML(fieldtext.Cell(cc.Value)) },
		Text: func(CellContext) string { panic("secret-text") },
	}, nil)
	body := string(x.ui.List("deals").Base("/deals").Columns("name", "amount").RenderCtx(x.userCtx("/deals", "", "u1")))
	if strings.Contains(body, "secret-text") || strings.Contains(buf.String(), "secret-text") {
		t.Fatalf("panic text leaked:\nbody: %s\nlog: %s", body, buf.String())
	}
	if !strings.Contains(body, ">d1<") {
		t.Fatalf("row link does not fall back to the id:\n%s", body)
	}
}

// New refuses a FilesURL that is not a same-origin path starting and ending with /.
func TestSweepCoreNewRefusesBadFilesURL(t *testing.T) {
	for _, bad := range []string{"uploads/", "/uploads", "//evil.example/", "https://evil.example/"} {
		x := newTestHost(t, map[string]entity.EntityConfig{"deals": dealsConfig()}, nil)
		if _, err := New(x.host, Extensions{FilesURL: bad}); err == nil {
			t.Errorf("New accepted FilesURL %q", bad)
		}
	}
	x := newTestHost(t, map[string]entity.EntityConfig{"deals": dealsConfig()}, nil)
	if _, err := New(x.host, Extensions{FilesURL: "/uploads/"}); err != nil {
		t.Fatalf("New refused /uploads/: %v", err)
	}
}

// New keeps its own copy of Extensions: changing the caller's maps afterwards changes no screen.
func TestSweepCoreNewClonesExtensions(t *testing.T) {
	x := sweepKindUI(t, Kind{Cell: func(cc CellContext) render.HTML { return render.HTML("<b>" + fieldtext.Cell(cc.Value) + "</b>") }}, func(ext *Extensions) {
		ext.Kinds["probe"] = Kind{Cell: func(CellContext) render.HTML { return "MUTATED" }}
	})
	body := string(x.ui.List("deals").Base("/deals").Columns("name", "amount").RenderCtx(x.userCtx("/deals", "", "u1")))
	if strings.Contains(body, "MUTATED") {
		t.Fatalf("a change to the caller's Extensions after New reached the screen:\n%s", body)
	}
}

// New refuses an extension tab whose key is one of the built-in tabs.
func TestSweepCoreNewRefusesBuiltinTabKey(t *testing.T) {
	for _, key := range builtinTabs {
		x := newTestHost(t, map[string]entity.EntityConfig{"deals": dealsConfig()}, nil)
		ext := Extensions{Entities: map[string]Extension{"deals": {Tabs: []Tab{{Key: key, Build: func(TabContext) (component.Component, error) { return nil, nil }}}}}}
		if _, err := New(x.host, ext); err == nil {
			t.Errorf("New accepted an extension tab keyed %q", key)
		}
	}
}

// A kind's Cell runs as the caller: under a back office's elevation it still cannot count rows its roles cannot read.
func TestSweepCoreKindCellIsNotElevated(t *testing.T) {
	ran, leaked := false, false
	var x *testUI
	ext := Extensions{Kinds: map[string]Kind{"probe": {Cell: func(cc CellContext) render.HTML {
		ran = true
		if _, ok := x.ui.Count(cc.Ctx, "invoices", ""); ok {
			leaked = true
		}
		return render.HTML(fieldtext.Cell(cc.Value))
	}}}}
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read"}}
	inv.Display.Fields["memo"] = entity.FieldDisplay{Input: "probe"}
	ents["invoices"] = inv
	x = newTestUIExt(t, ents, invoiceRows(), ext, withAPI(map[string]string{"invoices": "/api/invoices"}))
	policy := access.NewRolePolicy()
	policy.Register("invoices:read")
	x.ui.List("invoices").Base("/invoices").Columns("number", "memo").RenderCtx(elevatedClerk(x, policy, "/invoices", ""))
	if !ran {
		t.Fatal("setup: the elevated list never drew the kind's cell")
	}
	if leaked {
		t.Fatal("SECURITY: a kind's Cell counted rows with the back office's elevation")
	}
}
