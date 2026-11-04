# Source discovery and incomplete analysis

Static gates share `internal/sourcewalk.Walk`. It follows symlinked source
directories once, avoids cycles, and skips dependency caches, hidden and
underscore subtrees, and standard build-output directories. An explicitly
selected root is scanned even when its name normally denotes build output.

Visitors use the `io/fs.WalkDirFunc` controls: `fs.SkipDir` skips the current
directory, including when returned from a directory error callback, without
abandoning its siblings. On a non-directory entry it skips the remaining
entries in that entry's parent directory. `fs.SkipAll` stops discovery
successfully. Either sentinel returned for the root is consumed; other
visitor errors are returned unchanged.

## Contracts pass

`framework/contracts.NewPass` requires a readable directory root. A root
walk error is fatal; unreadable child directories do not prevent sibling
discovery. Unreadable Go, CSS, and JavaScript files remain visible in
`Pass.Unparsed()` with their slash-separated relative paths and read errors.
Readable siblings are still analyzed, and the report includes the unreadable
file count alongside parse failures in JSON and text.

`NewPass` can construct an empty pass for discovery-only consumers, but
`contracts.Run` returns an error when no readable source files were
discovered. Empty directories, config-only trees, excluded-only trees, and
trees whose source files are all unreadable cannot produce a clean report.
The verify command and build gate already treat this pipeline error as
failure. See the [contracts reference](../framework/docs/content/contracts.md).

## Maintenance notes

### 2026-11-04 - Preserve error-callback controls
- Scope: tooling/source-discovery
- Trigger: an unreadable directory aborted contracts before readable siblings.
- Approach: consume directory skips at every error callback and normalize root controls.
- Evidence: `go test -count=1 ./internal/sourcewalk ./framework/contracts -run 'TestWalkErrorControls|TestPassDiscoversSiblingOfUnreadableDirectory'`.
- Next time: run permission regressions as a non-root Unix user; mutation-test callback controls.
- Status: active

### 2026-11-04 - Keep incomplete contracts analysis honest
- Scope: contracts/discovery
- Trigger: root/read failures and empty discovery produced apparent success.
- Approach: reject root errors, retain file read failures, and reject empty analysis in `Run`.
- Evidence: `TestPassRejectsUnreadableRoot`, `TestPassRecordsUnreadableSources`, `TestRunRejectsZeroSourceFiles`, and `TestRunReportsUnreadableSources`.
- Next time: preserve empty-pass inspection for wiring tests and style generation; fail only analysis.
- Status: active
