package appspec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeSpecDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A directory spec split by concern: the app block in one file, the theme
// in another, hooks in a third. The merge used to swap the whole app
// struct for the theme file's (dropping auth) and never carried hooks.
func TestDirMergeKeepsAppKeysAndHooks(t *testing.T) {
	dir := writeSpecDir(t, map[string]string{
		"00-app.yml": `app:
  name: Demo
  module: ex.com/demo
  api_prefix: ""
  auth:
    enabled: true
    dev_mode: false
`,
		"10-theme.yml": `app:
  theme:
    primary: "#112233"
`,
		"20-hooks.yml": `entities:
  - name: posts
    fields:
      - name: title
        type: string
hooks:
  - id: check_title
    entity: posts
    when: before_create
    handler: CheckTitle
`,
	})
	bp, err := LoadAppSpecPath(dir, false)
	if err != nil {
		t.Fatalf("dir merge: %v", err)
	}
	if bp.App.Name != "Demo" || bp.App.Module != "ex.com/demo" {
		t.Fatalf("app name/module lost: %+v", bp.App)
	}
	if !bp.App.Auth.Enabled || bp.App.Auth.DevMode {
		t.Fatalf("app.auth lost: %+v", bp.App.Auth)
	}
	// The theme file's app: block must not re-apply the decode default.
	if bp.App.APIPrefix != "" {
		t.Fatalf("api_prefix opt-out lost: %q", bp.App.APIPrefix)
	}
	if bp.App.Theme["primary"] != "#112233" {
		t.Fatalf("theme lost: %v", bp.App.Theme)
	}
	if len(bp.Hooks) != 1 || bp.Hooks[0].Handler != "CheckTitle" {
		t.Fatalf("hooks lost: %+v", bp.Hooks)
	}
}

// A key two files both set is a conflict, not a silent last-wins.
func TestDirMergeRefusesAppKeyInTwoFiles(t *testing.T) {
	dir := writeSpecDir(t, map[string]string{
		"00-app.yml":   "app:\n  name: Demo\n  auth:\n    enabled: true\n",
		"10-other.yml": "app:\n  auth:\n    enabled: false\n",
	})
	_, err := LoadAppSpecPath(dir, false)
	if err == nil {
		t.Fatal("want an error for app.auth.enabled set in two files")
	}
	for _, want := range []string{"app.auth.enabled", "00-app.yml", "10-other.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
}

// Every list section of AppSpec reaches the merged spec. A field added
// later fails here until MergeAppSpecs appends it.
func TestMergeAppSpecsAppendsEverySlice(t *testing.T) {
	var b AppSpec
	bv := reflect.ValueOf(&b).Elem()
	var slices []string
	for i := 0; i < bv.NumField(); i++ {
		f := bv.Type().Field(i)
		if !f.IsExported() || f.Type.Kind() != reflect.Slice {
			continue
		}
		field := bv.Field(i)
		if !field.CanSet() {
			t.Fatalf("field %s not settable", f.Name)
		}
		field.Set(reflect.MakeSlice(f.Type, 1, 1))
		slices = append(slices, f.Name)
	}
	merged := MergeAppSpecs(AppSpec{}, b)
	mv := reflect.ValueOf(merged)
	for _, name := range slices {
		if got := mv.FieldByName(name).Len(); got != 1 {
			t.Errorf("MergeAppSpecs dropped %s: len %d", name, got)
		}
	}
}
