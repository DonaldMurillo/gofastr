package embed

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
	agentsinv.Register(agentsinv.Entry{
		Name:       "embed",
		Kind:       agentsinv.KindBattery,
		ImportPath: "github.com/DonaldMurillo/gofastr/battery/embed",
		Markdown:   agentsMarkdown,
	})
}
