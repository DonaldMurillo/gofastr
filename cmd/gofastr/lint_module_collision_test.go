package main

import (
	"strings"
	"testing"
)

// Copying an in-repo example blueprint out of the repo is the documented way
// to start from an example, and every one of them declares an app.module under
// the framework's own module path. Outside this repo that path is claimed by
// both the local module and the framework dependency, so `go build` fails with
// "ambiguous import" — an error no go.mod edit can resolve, and one the
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
// a collision — only a true path-segment descendant is. "…/gofastr-app" lives
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

// Every blueprint shipped in examples/ is expected to collide — they are
// generated inside this module, where the local package legitimately wins.
// Pinning that keeps the lint honest about which case it is warning on: if a
// shipped example ever stops colliding, the warning a copying reader depends on
// is no longer being exercised by anything.
func TestShippedExampleBlueprintsAreTheCollidingCase(t *testing.T) {
	paths := exampleBlueprints(t)
	colliding := 0
	for _, path := range paths {
		bp, err := loadBlueprint(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		if len(lintModuleCollision(bp)) > 0 {
			colliding++
		}
	}
	if colliding == 0 {
		t.Fatalf("no shipped blueprint declares a framework-subpath module — the copy-out-of-repo warning is unexercised across %d examples", len(paths))
	}
}
