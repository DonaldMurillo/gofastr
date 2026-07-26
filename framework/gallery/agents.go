package gallery

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
	agentsinv.Register(agentsinv.Entry{
		Name:       "gallery",
		Kind:       agentsinv.KindFramework,
		ImportPath: "github.com/DonaldMurillo/gofastr/framework/gallery",
		Markdown:   agentsMarkdown,
	})
}
