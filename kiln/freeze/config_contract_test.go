package freeze_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestFreezeEntityLLMMD(t *testing.T) {
	for _, config := range []string{`{}`, `{"llm_md":false}`, `{"llm_md":true}`} {
		t.Run(config, func(t *testing.T) {
			w := world.New()
			if err := json.Unmarshal([]byte(config), &w.App); err != nil {
				t.Fatal(err)
			}
			buf, err := freeze.AppSpecYAML(w)
			if err != nil {
				t.Fatal(err)
			}
			node, err := coreyaml.Parse(string(buf))
			if err != nil {
				t.Fatal(err)
			}
			got := node.Map["app"].Map["entity_llm_md"]
			if got == nil || got.Value != w.App.LLMMD {
				t.Fatalf("entity_llm_md missing or wrong for LLMMD=%v:\n%s", w.App.LLMMD, buf)
			}
			ui := node.Map["app"].Map["llm_md"]
			if (ui != nil) != w.App.LLMMD {
				t.Fatal("UI llm_md emission changed")
			}
		})
	}
}

func TestFreezeDebugEndpoints(t *testing.T) {
	var specs [2][]byte
	for i, enabled := range []bool{false, true} {
		w := world.New()
		w.App.DebugEndpoints = enabled
		dir := t.TempDir()
		if err := freeze.Freeze(w, dir); err != nil {
			t.Fatalf("debug_endpoints=%v: %v", enabled, err)
		}
		spec, err := os.ReadFile(filepath.Join(dir, "gofastr.yml"))
		if err != nil {
			t.Fatal(err)
		}
		specs[i] = spec
		warnings := freeze.GraduationWarnings(w)
		if enabled != (len(warnings) == 1 && strings.Contains(warnings[0], "debug routes are not carried")) {
			t.Fatalf("debug_endpoints=%v: warnings %q", enabled, warnings)
		}
	}
	if string(specs[0]) != string(specs[1]) {
		t.Fatalf("debug_endpoints changed the app spec:\n%s\n---\n%s", specs[0], specs[1])
	}
}

func TestRefusedFreezeCreatesNoDirectory(t *testing.T) {
	w := world.New()
	w.App.JSONCase = "snake"
	root := t.TempDir()
	if err := freeze.Freeze(w, filepath.Join(root, "a", "b", "out")); err == nil {
		t.Fatal("want refusal")
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Fatalf("refused freeze created its output tree: %v", err)
	}
}
