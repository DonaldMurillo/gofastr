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

## Maintenance notes

### 2026-11-04 - Preserve error-callback controls
- Scope: tooling/source-discovery
- Trigger: an unreadable directory aborted contracts before readable siblings.
- Approach: consume directory skips at every error callback and normalize root controls.
- Evidence: `go test -count=1 ./internal/sourcewalk ./framework/contracts -run 'TestWalkErrorControls|TestPassDiscoversSiblingOfUnreadableDirectory'`.
- Next time: run permission regressions as a non-root Unix user; mutation-test callback controls.
- Status: active
