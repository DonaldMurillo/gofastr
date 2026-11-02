package releasetest

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// v096Note finds a v0.96.0 note by the start of its change text, so the
// test does not depend on the note's position in a release file several
// branches append to.
func v096Note(t *testing.T, prefix string) *upgrade.Note {
	t.Helper()
	reg := scantest.Registry(t)
	i := slices.IndexFunc(reg.Releases, func(r upgrade.Release) bool { return r.Version == "v0.96.0" })
	if i < 0 {
		t.Fatal("registry has no v0.96.0")
	}
	for j, n := range reg.Releases[i].Notes {
		if strings.HasPrefix(n.Change, prefix) {
			return scantest.Note(t, reg, "v0.96.0", j)
		}
	}
	t.Fatalf("v0.96.0 has no note starting %q", prefix)
	return nil
}

// A custom A2A store still declaring the old TrimTerminalTasks(ctx,
// owner, keep) is found; a type that only shares the method name and
// implements no RetentionTrimmer is not.
func TestV096RetentionTrimmerTierNote(t *testing.T) {
	n := v096Note(t, "BREAKING for custom A2A stores - a2a.RetentionTrimmer.TrimTerminalTasks takes a tier")
	kit := map[string]string{"core/a2a/a2a.go": `package a2a

import "context"

type RetentionTrimmer interface {
	TrimTerminalTasks(ctx context.Context, owner string, keep int) error
	TrimPushConfigs(ctx context.Context, owner, taskID string, keep int) error
}
`}
	const store = `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/a2a"
)

type myStore struct{}

func (myStore) TrimTerminalTasks(_ context.Context, owner string, keep int) error { return nil }
func (myStore) TrimPushConfigs(context.Context, string, string, int) error   { return nil }

var _ a2a.RetentionTrimmer = myStore{}

func main() {}
`
	app := scantest.App(t, map[string]string{"main.go": store}, scantest.Options{Kit: kit})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) == 0 {
		t.Error("an old-shape TrimTerminalTasks on a RetentionTrimmer store was not found")
	}

	const unrelated = `package main

import (
	"context"

	_ "github.com/DonaldMurillo/gofastr/core/a2a"
)

type cache struct{}

func (cache) TrimTerminalTasks(context.Context, string, int) error { return nil }

func main() {}
`
	app = scantest.App(t, map[string]string{"main.go": unrelated}, scantest.Options{Kit: kit})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Errorf("fires on a type that is no RetentionTrimmer: %v", got)
	}
}
