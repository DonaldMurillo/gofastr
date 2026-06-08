package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewUsagePrints(t *testing.T) {
	out := covT_capStdout(t, newUsage)
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "entity") {
		t.Fatalf("newUsage output: %s", out)
	}
}

func TestRunNewDispatchesAllResources(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)

	covT_capStdout(t, func() { runNew([]string{"entity", "Widget", "name:string"}) })
	if _, err := os.Stat(filepath.Join(dir, "entities", "widget.json")); err != nil {
		t.Fatalf("entity not created: %v", err)
	}

	covT_capStdout(t, func() { runNew([]string{"handler", "Ping", "--method=POST", "--path=/p"}) })
	if _, err := os.Stat(filepath.Join(dir, "ping_handler.go")); err != nil {
		t.Fatalf("handler not created: %v", err)
	}

	out := covT_capStdout(t, func() { runNew([]string{"route", "/x", "--method=POST", "--handler=h"}) })
	if !strings.Contains(out, "app.Router().Handle") {
		t.Fatalf("route snippet missing: %s", out)
	}
}

func TestRunNewHelp(t *testing.T) {
	out := covT_capStdout(t, func() { runNew([]string{"--help"}) })
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("help: %s", out)
	}
}

func TestRunNewNoArgsExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNew(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewUnknownResourceExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNew([]string{"frobnicate"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewEntityNoNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewEntity(nil, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewEntityInvalidNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewEntity([]string{"../evil"}, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewHandlerNoNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewHandler(nil, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewHandlerInvalidNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewHandler([]string{"a/b"}, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewRouteNoArgsExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewRoute(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestExtractOverwriteFlag(t *testing.T) {
	ov, rest := extractOverwriteFlag([]string{"a", "-overwrite", "b"})
	if !ov || len(rest) != 2 || rest[0] != "a" || rest[1] != "b" {
		t.Fatalf("got ov=%v rest=%v", ov, rest)
	}
	ov2, rest2 := extractOverwriteFlag([]string{"x", "--overwrite"})
	if !ov2 || len(rest2) != 1 {
		t.Fatalf("got ov=%v rest=%v", ov2, rest2)
	}
	ov3, _ := extractOverwriteFlag([]string{"x"})
	if ov3 {
		t.Fatalf("expected no overwrite")
	}
}

func TestParseFieldArgVariants(t *testing.T) {
	if got := parseFieldArg("name"); !strings.Contains(got, `"type": "string"`) {
		t.Fatalf("bare name: %s", got)
	}
	if got := parseFieldArg("age:int:required"); !strings.Contains(got, `"required": true`) || !strings.Contains(got, `"int"`) {
		t.Fatalf("required: %s", got)
	}
	if got := parseFieldArg("slug:string:unique"); !strings.Contains(got, `"unique": true`) {
		t.Fatalf("unique: %s", got)
	}
}

func TestSchemaFieldTypeAll(t *testing.T) {
	cases := map[string]string{
		"string": "string", "text": "string", "int": "int", "integer": "int",
		"float": "float", "float64": "float", "decimal": "float",
		"bool": "bool", "boolean": "bool", "datetime": "datetime", "timestamp": "datetime",
		"time": "datetime", "date": "date", "json": "json", "jsonb": "json",
		"uuid": "uuid", "blob": "blob", "bytes": "blob", "weird": "string",
	}
	for in, want := range cases {
		if got := schemaFieldType(in); got != want {
			t.Errorf("schemaFieldType(%q)=%q want %q", in, got, want)
		}
	}
}
