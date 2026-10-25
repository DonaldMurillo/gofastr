package packcmd

import (
	"path"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// endpointHandlerFiles lists the files bp's endpoints link. The handler
// bodies are the developer's, so pack's check never compares them.
func endpointHandlerFiles(bp appspec.AppSpec) map[string]bool {
	out := map[string]bool{}
	for _, e := range bp.Endpoints {
		out[path.Clean(e.HandlerFile())] = true
	}
	return out
}
