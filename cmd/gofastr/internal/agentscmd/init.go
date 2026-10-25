package agentscmd

// DevLoopGuidance is the dev-loop paragraph the generated CLAUDE.md and
// AGENTS.md both carry near the top. Agents that miss it reach for
// `go run .`, which never sets GOFASTR_DEV=1 and so never reloads;
// evals/dev-loop measures whether they find it.
const DevLoopGuidance = "Start `gofastr dev` once, in the background, and leave it running. It\n" +
	"rebuilds the app on every save and reloads the open browser tab, so you\n" +
	"never restart the server between edits. `go run .` and a binary you build\n" +
	"and launch yourself never reload: `gofastr dev` is the only command that\n" +
	"sets `GOFASTR_DEV=1`. Stop it when you finish.\n"
