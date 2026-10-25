package agentscmd

import (
	"fmt"
)

func printAgentsUsage() {
	fmt.Println(`Usage: gofastr agents [init|sync|skill]

Generate or refresh AGENTS.md and the per-battery detail files.

  init    write a fresh AGENTS.md (errors if it exists; use 'sync' to refresh)
  sync    regenerate the battery inventory section in an existing AGENTS.md
  skill   emit a host-skill snippet for an agent that edits this project`)
}
