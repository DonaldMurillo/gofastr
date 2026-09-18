package local

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
	agentsinv.Register(agentsinv.Entry{
		Name:       "local",
		Kind:       agentsinv.KindFramework,
		ImportPath: "github.com/DonaldMurillo/gofastr/framework/local",
		Markdown:   agentsMarkdown,
	})
}
