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
	for _, enabled := range []bool{false, true} {
		w := world.New()
		w.App.DebugEndpoints = enabled
		dir := t.TempDir()
		err := freeze.Freeze(w, dir)
		if !enabled {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "debug_endpoints") || !strings.Contains(err.Error(), "framework.AppConfig.DebugEndpoints") {
			t.Fatalf("want actionable debug_endpoints refusal, got %v", err)
		}
		for _, name := range []string{"gofastr.yml", "world.json"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Errorf("refused freeze wrote %s: %v", name, err)
			}
		}
	}
}
