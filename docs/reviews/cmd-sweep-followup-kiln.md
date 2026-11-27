## kiln (#923)

Each line is `file: mutation -> failing test`, run with `go test -count=1`.

- cmd/kiln/main.go: `kilnLoopbackBound = isLoopbackBindAddr(opts.addr)` deleted -> TestServeRunsBehindTheGuard
- cmd/kiln/main.go: runHTTP serves `l` instead of `originGuard(l)` -> TestServeRunsBehindTheGuard
- cmd/kiln/main.go: the logger writes to os.Stdout -> TestStdioModesWriteOnlyFrames
- cmd/kiln/agent_watcher.go: the journaled turn summary removed -> TestWatcherJournalsATurnSummary
- cmd/kiln/agent_watcher.go: the adapter read once before the loop instead of per message -> TestWatcherReadsTheAdapterPerMessage
- cmd/kiln/agent.go: runAgent builds the serve argv by hand (`--addr :port`) instead of calling serveArgs -> TestRunAgentStartsServeWithServeArgs (TestAgentServeBindsLoopback passes)

The deleted reasons for `--dangerously-skip-permissions`, `--no-session` and killGroup's `-pid` are decisions `claude-runs-without-permission-prompts`, `omp-keeps-no-session` and `kill-the-whole-group`.
