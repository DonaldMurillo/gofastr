# Source discovery and incomplete analysis

Static gates share `internal/sourcewalk.Walk`. It follows symlinked source
directories once, avoids cycles, and skips dependency caches, hidden and
underscore subtrees, and standard build-output directories. An explicitly
selected root is scanned even when its name normally denotes build output.
Callers with additional root-specific exclusions clean the root once before
walking and comparing it; `.` and `./` scan identically, and `.app/` is explicit.
Ordinary directories are visited before deferred directory links. A source
reachable through both an in-root path and an alias keeps its real relative
path, so changed-file report restriction cannot discard it under the alias.
External linked packages are still discovered once under their logical link path.
The excluded descendant names are `vendor`, `node_modules`, `dist`, `bin`,
`build`, `tmp`, and names beginning with `.` or `_`; link names are excluded
before their targets are inspected. Dangling and looping links are delivered
as non-directory entries, not traversal errors. Other link errors still reach
the visitor, and contracts records failures on source paths in `Unparsed`.

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

### 2026-11-04 - Prefer real paths before directory aliases
- Scope: contracts/changed-path-identity
- Trigger: an earlier alias consumed a tracked subtree and restriction dropped its findings.
- Approach: finish ordinary traversal before following queued directory links.
- Evidence: `TestPassPreservesChangedRealPathBeforeDirectoryAlias` covers sibling and nested aliases.
- Next time: combine a tracked edit, discovery, and report restriction in alias regressions.
- Status: active

### 2026-11-04 - Normalize roots before caller exclusions
- Scope: tooling/root-spelling
- Trigger: `./` and `.app/` were mistaken for excluded descendant directories.
- Approach: clean the caller root before both traversal and root comparisons.
- Evidence: `TestCLINormalizesExplicitRootSpelling`, `TestRegisteredBehaviorSourcesNormalizesRoot`, `TestScanProvenanceNormalizesRoot`.
- Next time: keep CLI, behavior discovery, and provenance spelling controls together.
- Status: active

### 2026-11-04 - Keep symlink failures source-aware
- Scope: tooling/source-discovery
- Trigger: dangling tooling links aborted gates and inaccessible source links disappeared.
- Approach: exclude dependency links before stat; retain unreadable source-link paths in contracts.
- Evidence: `TestWalkDanglingLoopingAndExcludedLinks`, `TestPassRecordsUnreadableSourceLinks`, `TestCLIIgnoresDanglingNonSourceLinks`.
- Next time: test dangling, looping, and permission-denied links separately with a readable sibling.
- Status: active

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
