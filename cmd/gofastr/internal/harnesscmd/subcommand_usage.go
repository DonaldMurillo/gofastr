package harnesscmd

import (
	"fmt"
)

func printHarnessUsage() {
	fmt.Println(`Usage: gofastr harness [flags]
       gofastr harness mcp               launch the harness as a stdio MCP server
       gofastr harness creds [add|list|delete]  manage encrypted API-key credentials

Boots the AI agent harness. Flags select the profile, transports, and
permission posture; see the harness architecture docs for the full list.`)
}
