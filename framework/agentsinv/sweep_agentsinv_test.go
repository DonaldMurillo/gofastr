package agentsinv

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/importcheck"
)

func TestSweepAllAndMissingReturnCopies(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	Register(Entry{Name: "b", Kind: KindBattery, Markdown: "x"})
	Register(Entry{Name: "a", Kind: KindBattery, Markdown: "x"})
	Register(Entry{Name: "gone", Kind: KindBattery})
	All()[0].Name = "changed"
	MissingMarkdown()[0].Name = "changed"
	if got := All(); got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("All after a caller wrote to its result = %v", got)
	}
	if got := MissingMarkdown(); got[0].Name != "gone" {
		t.Errorf("MissingMarkdown after a caller wrote to its result = %v", got)
	}
	Reset(t)
	if len(All()) != 0 || len(MissingMarkdown()) != 0 {
		t.Error("Reset left entries behind")
	}
}

func TestSweepAgentsinvLinksNoTesting(t *testing.T) {
	importcheck.Run(t, importcheck.Ban{
		Deny:      "testing",
		Primitive: "agentsinv",
		Why:       "agentsinv is linked into the gofastr CLI and every app that imports a battery, and testing would drag in its flag and pprof trees.",
		Instead:   "Take an interface such as TestingT.",
	})
}
