package notify

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
	agentsinv.Register(agentsinv.Entry{
		Name:       "notify",
		Kind:       agentsinv.KindBattery,
		ImportPath: "github.com/DonaldMurillo/gofastr/battery/notify",
		Markdown:   agentsMarkdown,
	})
}
