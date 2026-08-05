package main

// Executable-docs gate for the taught area hubs (screens_hubs.go).
//
// Every code sample on a taught hub is a contract: a reader pastes it and it
// must compile against the framework's real API. The /framework hub had
// drifted before — its entity and access-control samples used the pre-grouped
// flat EntityConfig shape (Public: / OwnerField: / Access: at the top level)
// after those fields moved onto ExposureConfig and ScopeConfig — and nothing
// caught it because no test compiled what the page renders.
//
// This gate renders the /framework hub, extracts every Go code block, wraps
// each fragment in the minimal host context a reader pasting it would already
// have (a func body for bare statements, plus the imports the snippet names),
// and compiles it against the local tree. A snippet that drifts fails CI here,
// not in a reader's editor.
//
// It reuses the extraction + compile machinery shared with the /get-started
// gate (get_started_compile_test.go): extractGetStartedGoBlocks and
// compileGetStartedSnippet are generic over the ui.CodeBlock chrome — only
// the per-fragment wrapping is hub-specific, because a hub page repeats
// filenames (two main.go blocks) so dispatch is by what the snippet says, not
// the filename it claims.
//
// Red-first: this gate was committed against the broken flat EntityConfig
// samples and watched fail before the grouped-shape fix landed.

import (
	"strings"
	"testing"
)

func TestFrameworkHubGoBlocksCompile(t *testing.T) {
	page := string(frameworkHub().Render())

	blocks := extractGetStartedGoBlocks(t, page)
	if len(blocks) == 0 {
		t.Fatal("no Go code blocks found on the /framework hub — extraction is broken or the page changed shape")
	}

	for _, b := range blocks {
		b := b
		t.Run(b.label, func(t *testing.T) {
			src, ok := wrapHubFrameworkSnippet(b.source)
			if !ok {
				t.Fatalf("no compile harness for hub code block %q — add one in wrapHubFrameworkSnippet\n--- source ---\n%s", b.filename, b.source)
			}
			compileGetStartedSnippet(t, src, b.label)
		})
	}
}

// wrapHubFrameworkSnippet wraps an extracted hub snippet in the package
// context + imports a reader pasting it would already have, keyed by what the
// snippet contains (a hub page repeats filenames — two main.go blocks — so the
// filename alone can't pick the harness). Each branch supplies exactly the
// declarations the fragment references and no more, so the snippet compiles
// for the reasons a reader's file would. The default returns false so a new Go
// block with no harness fails loudly instead of silently compiling the wrong
// thing.
func wrapHubFrameworkSnippet(source string) (string, bool) {
	switch {
	// Entities + Access-control concepts: a bare app.Entity(...) statement.
	// Needs a host function holding the *framework.App and framework (EntityConfig
	// / ExposureConfig / ScopeConfig / AccessControl are aliased at the package
	// root). core/schema is pulled in only when the snippet references it — the
	// Entities sample declares a field list, the Access-control one does not, and
	// an unconditional import would be "imported and not used" for the latter.
	case strings.Contains(source, "app.Entity("):
		imports := "\t\"github.com/DonaldMurillo/gofastr/framework\"\n"
		if strings.Contains(source, "schema.") {
			imports = "\t\"github.com/DonaldMurillo/gofastr/core/schema\"\n" + imports
		}
		return "package main\n\n" +
			"import (\n" + imports + ")\n\n" +
			"func registerSample(app *framework.App) {\n" +
			source + "\n" +
			"}\n\nfunc main() {}\n", true

	// Auth concept: references db, fwApp, the auth battery, and log.
	case strings.Contains(source, "authMgr"):
		return "package main\n\n" +
			"import (\n" +
			"\t\"database/sql\"\n" +
			"\t\"log\"\n" +
			"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/battery/auth\"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/framework\"\n" +
			")\n\n" +
			"func setupAuth(db *sql.DB, fwApp *framework.App) {\n" +
			source + "\n" +
			"}\n\nfunc main() {}\n", true

	// Migrations concept: references db, app.Registry, framework.AutoMigrate.
	case strings.Contains(source, "AutoMigrate"):
		return "package main\n\n" +
			"import (\n" +
			"\t\"database/sql\"\n" +
			"\t\"log\"\n" +
			"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/framework\"\n" +
			")\n\n" +
			"func migrateAll(db *sql.DB, app *framework.App) {\n" +
			source + "\n" +
			"}\n\nfunc main() {}\n", true

	// Components concept: a ui.PageHeader(...) call expression → wrap as a
	// return value so it is a statement, not a bare expression at func scope.
	case strings.Contains(source, "ui.PageHeader"):
		return "package main\n\n" +
			"import (\n" +
			"\t\"github.com/DonaldMurillo/gofastr/core/render\"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n" +
			")\n\n" +
			"func pageHeaderSample() render.HTML {\n" +
			"\treturn " + source + "\n" +
			"}\n\nfunc main() {}\n", true

	// Theming concept: references a `site` host app's WithTheme + the theme
	// overrides. `site` is the *core-ui/app.App a host already constructed.
	case strings.Contains(source, "theme.Default"):
		return "package main\n\n" +
			"import (\n" +
			"\t\"github.com/DonaldMurillo/gofastr/core-ui/app\"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/framework/ui/theme\"\n" +
			")\n\n" +
			"func applyTheme(site *app.App) {\n" +
			source + "\n" +
			"}\n\nfunc main() {}\n", true

	default:
		return "", false
	}
}
