package headless

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
	agentsinv.Register(agentsinv.Entry{
		Name:       "headless",
		Kind:       agentsinv.KindFramework,
		ImportPath: "github.com/DonaldMurillo/gofastr/framework/headless",
		Markdown:   agentsMarkdown,
	})
}
