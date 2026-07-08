package ui

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
	agentsinv.Register(agentsinv.Entry{
		Name:       "ui",
		Kind:       agentsinv.KindFramework,
		ImportPath: "github.com/DonaldMurillo/gofastr/framework/ui",
		Markdown:   agentsMarkdown,
	})
}
