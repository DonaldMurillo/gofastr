package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Copying an in-repo example blueprint out of the repo is the documented way
// to start from an example, and every one of them declares an app.module under
// the framework's own module path. Outside this repo that path is claimed by
// both the local module and the framework dependency, so `go build` fails with
// "ambiguous import": an error no go.mod edit can resolve, and one the
// generator's own "Next steps" used to lead the reader deeper into by printing
// `go mod init <the colliding path>`.
func TestModuleCollisionFlagsFrameworkSubpaths(t *testing.T) {
	for _, module := range []string{
		"github.com/DonaldMurillo/gofastr",
		"github.com/DonaldMurillo/gofastr/examples/blog",
		"github.com/DonaldMurillo/gofastr/anything/deeper",
	} {
		bp := Blueprint{}
		bp.App.Module = module
		findings := lintModuleCollision(bp)
		if len(findings) != 1 {
			t.Fatalf("module %q: got %d findings, want 1", module, len(findings))
		}
		msg := findings[0].Message()
		if !strings.Contains(msg, "ambiguous import") {
			t.Errorf("module %q: message must name the toolchain error the user will see:\n%s", module, msg)
		}
		if !strings.Contains(msg, module) {
			t.Errorf("module %q: message must quote the offending path:\n%s", module, msg)
		}
	}
}

// A module path that merely shares a prefix string with the framework's is not
// a collision: only a true path-segment descendant is. "…/gofastr-app" lives
// beside the framework module, not inside it, and builds fine.
func TestModuleCollisionIgnoresNonSubpaths(t *testing.T) {
	for _, module := range []string{
		"local/blog",
		"github.com/someone/gofastr",
		"github.com/DonaldMurillo/gofastr-app",
		"github.com/DonaldMurillo/other/gofastr",
		"example.com/team/service",
	} {
		bp := Blueprint{}
		bp.App.Module = module
		if findings := lintModuleCollision(bp); len(findings) != 0 {
			t.Errorf("module %q must not be flagged, got: %s", module, findings[0].Message())
		}
	}
}

// Every blueprint shipped in examples/ declares an app.module under the
// framework's own path, so every one of them is a fixture for this rule —
// asserting against one, or against "at least one", leaves the rest untested
// and lets a silent change to any of them go unnoticed. Pinning all of them
// keeps the lint honest about which case it warns on: a shipped example that
// stops colliding is a reader copying it out of the repo into a build error the
// warning no longer covers.
func TestShippedExampleBlueprintsAreTheCollidingCase(t *testing.T) {
	// Globbed here rather than through exampleBlueprints, which SKIPS on an
	// empty match. A skip reads as a pass, so a broken glob would have retired
	// this rule's entire fixture set silently. These blueprints are committed;
	// finding none of them is a failure, not a reason to stand down.
	paths, err := filepath.Glob("../../examples/*/gofastr.yml")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no shipped blueprints found: this rule has no fixtures and proves nothing")
	}
	for _, path := range paths {
		bp, lerr := loadBlueprint(path)
		if lerr != nil {
			t.Fatalf("load %s: %v", path, lerr)
		}
		findings := lintModuleCollision(bp)
		if len(findings) != 1 {
			t.Errorf("%s declares module %q and got %d findings, want 1 — the copy-out-of-repo warning is unexercised for this example",
				path, bp.App.Module, len(findings))
			continue
		}
		if msg := findings[0].Message(); !strings.Contains(msg, bp.App.Module) {
			t.Errorf("%s: finding must quote the offending path %q:\n%s", path, bp.App.Module, msg)
		}
	}
}
