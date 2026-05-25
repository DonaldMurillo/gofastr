package agentsinv_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

func TestRegisterAndAll(t *testing.T) {
	agentsinv.Reset(t) // expose a test-only reset to keep cases independent

	agentsinv.Register(agentsinv.Entry{
		Name:       "demo",
		Kind:       agentsinv.KindBattery,
		ImportPath: "github.com/example/demo",
		Markdown:   "# demo\n",
	})

	got := agentsinv.All()
	if len(got) != 1 {
		t.Fatalf("len(All())=%d, want 1", len(got))
	}
	if got[0].Name != "demo" || got[0].Kind != agentsinv.KindBattery {
		t.Fatalf("entry mismatch: %+v", got[0])
	}
}

// Empty markdown must NOT panic — that would kill the gofastr binary
// at startup for every subcommand, including ones unrelated to agents
// (build / dev / migrate / version). Track the missing entry so it
// surfaces via MissingMarkdown() instead.
func TestRegisterRecordsEmptyMarkdownWithoutPanicking(t *testing.T) {
	agentsinv.Reset(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register must not panic on empty Markdown; got: %v", r)
		}
	}()
	agentsinv.Register(agentsinv.Entry{Name: "broken", Kind: agentsinv.KindBattery, ImportPath: "p"})

	// All() must skip the broken entry.
	got := agentsinv.All()
	if len(got) != 0 {
		t.Fatalf("broken entry leaked into All(): %+v", got)
	}

	// MissingMarkdown() must report the broken battery so `gofastr
	// agents init/sync` can warn the developer.
	miss := agentsinv.MissingMarkdown()
	if len(miss) != 1 || miss[0].Name != "broken" {
		t.Fatalf("MissingMarkdown=%+v; want one entry named 'broken'", miss)
	}
}

func TestAllReturnsSortedByKindThenName(t *testing.T) {
	agentsinv.Reset(t)
	agentsinv.Register(agentsinv.Entry{Name: "zeta", Kind: agentsinv.KindBattery, ImportPath: "p1", Markdown: "x"})
	agentsinv.Register(agentsinv.Entry{Name: "alpha", Kind: agentsinv.KindFramework, ImportPath: "p2", Markdown: "x"})
	agentsinv.Register(agentsinv.Entry{Name: "alpha", Kind: agentsinv.KindBattery, ImportPath: "p3", Markdown: "x"})

	got := agentsinv.All()
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	// Framework entries first, then batteries; alphabetical within a kind.
	want := []string{"framework:alpha", "battery:alpha", "battery:zeta"}
	for i, e := range got {
		k := string(e.Kind) + ":" + e.Name
		if !strings.EqualFold(k, want[i]) {
			t.Fatalf("entry %d = %q, want %q", i, k, want[i])
		}
	}
}
