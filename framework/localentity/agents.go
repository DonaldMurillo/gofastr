package localentity

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
	agentsinv.Register(agentsinv.Entry{
		Name:       "localentity",
		Kind:       agentsinv.KindFramework,
		ImportPath: "github.com/DonaldMurillo/gofastr/framework/localentity",
		Markdown:   agentsMarkdown,
	})
}
