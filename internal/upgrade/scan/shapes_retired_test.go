package scan

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// An optional interface (app.ScreenTitler) has no assertion to break:
// against the bumped kit, a method still spelling the old shape no
// longer implements the interface and nothing fails to compile. The
// shapes entry still finds it, because the method itself is the only
// part of the interface the type misses.
func TestShapesDefRetiredSignatureHits(t *testing.T) {
	src := `package main

import (
	"context"

	_ "example.com/kit/queue"
)

type mem struct{}

func (m *mem) Ack(ctx context.Context, jobID string) error { return nil }

func main() {}
`
	res := mustRun(t, newWorkspace(t, shapeQueueNewKit, map[string]string{"main.go": src}), shapeAckNote)
	wantHits(t, res, shapeAckNote, hitAt(src, "Ack(ctx", "main.go",
		shapeAckSym.String()+" shape func(ctx context.Context, jobID string) error"))
}

var shapeStoreKit = map[string]string{"store/store.go": `package store

type Store interface {
	Get(key string) string
	Put(key string, n int) error
}
`}

// A type that misses another of the interface's methods is not an
// implementation: a same-named method on it is a different thing.
func TestShapesDefRetiredNeedsOtherMethods(t *testing.T) {
	sym := upgrade.Symbol{Pkg: "example.com/kit/store", Name: "Store", Member: "Put"}
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: sym, Type: regexp.MustCompile(`^func\(key string, v string\) error$`)},
	}}}
	src := `package main

import _ "example.com/kit/store"

type impl struct{}

func (impl) Get(key string) string          { return "" }
func (impl) Put(key string, v string) error { return nil }

type other struct{}

func (other) Put(key string, v string) error { return nil }

func main() {}
`
	res := mustRun(t, newWorkspace(t, shapeStoreKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "Put(key string, v string) error { return nil }\n\ntype other", "main.go",
		sym.String()+" shape func(key string, v string) error"))
}
