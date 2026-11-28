package packcmd

import (
	"path"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func endpointHandlerFiles(bp appspec.AppSpec) map[string]bool {
	out := map[string]bool{}
	for _, e := range bp.Endpoints {
		out[path.Clean(e.HandlerFile())] = true
	}
	return out
}
