package agentscmd

// DevLoopGuidance is the dev-loop paragraph near the top of the generated
// CLAUDE.md and AGENTS.md.
const DevLoopGuidance = "Start `gofastr dev` once, in the background, and leave it running. It\n" +
	"rebuilds the app on every save and reloads the open browser tab, so you\n" +
	"never restart the server between edits. `go run .` and a binary you build\n" +
	"and launch yourself never reload: `gofastr dev` is the only command that\n" +
	"sets `GOFASTR_DEV=1`. Stop it when you finish.\n"
