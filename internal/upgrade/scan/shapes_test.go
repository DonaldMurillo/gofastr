package scan

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

var shapeNewLayoutSym = upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "NewLayout"}
var shapeInputSym = upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormFieldConfig", Member: "Input"}

// shapeOldKit and shapeNewKit differ only in the shapes of two symbols
// that keep their names: NewLayout's signature and FormFieldConfig's
// Input field. A shapes entry written for the old shape must hit the
// app against the first kit and stay silent against the second.
var shapeOldKit = map[string]string{
	"render/render.go": "package render\n\ntype HTML string\n",
	"ui/ui.go": `package ui

import "example.com/kit/render"

type Layout struct{}

func NewLayout(name string) *Layout { return nil }

type FormFieldConfig struct{ Input render.HTML }
`,
}

var shapeNewKit = map[string]string{
	"render/render.go": "package render\n\ntype HTML string\n",
	"ui/ui.go": `package ui

import "example.com/kit/render"

type LayoutSpec struct{}

type LayoutTree struct{}

func NewLayout(name string, spec LayoutSpec, build func() string) *LayoutTree { return nil }

type FormFieldConfig struct{ Input func(name string) render.HTML }
`,
}

func TestShapesFuncOldSignatureHits(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

var shell = ui.NewLayout("shell")
`
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: shapeNewLayoutSym, Type: regexp.MustCompile(`^func\(name string\) \*ui\.Layout$`)},
	}}}
	res := mustRun(t, newWorkspace(t, shapeOldKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "NewLayout", "main.go",
		shapeNewLayoutSym.String()+" shape func(name string) *ui.Layout"))
}

func TestShapesFuncNewSignatureSilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

var shell = ui.NewLayout("shell", ui.LayoutSpec{}, func() string { return "" })
`
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: shapeNewLayoutSym, Type: regexp.MustCompile(`^func\(name string\) \*ui\.Layout$`)},
	}}}
	res := mustRun(t, newWorkspace(t, shapeNewKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestShapesFieldOldTypeHits(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

var email = ui.FormFieldConfig{Input: "<input type=email>"}
`
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: shapeInputSym, Type: regexp.MustCompile(`^render\.HTML$`)},
	}}}
	res := mustRun(t, newWorkspace(t, shapeOldKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "Input", "main.go",
		shapeInputSym.String()+" shape render.HTML"))
}

func TestShapesFieldNewTypeSilent(t *testing.T) {
	src := `package main

import (
	"example.com/kit/render"
	"example.com/kit/ui"
)

var email = ui.FormFieldConfig{Input: func(name string) render.HTML { return "" }}
`
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: shapeInputSym, Type: regexp.MustCompile(`^render\.HTML$`)},
	}}}
	res := mustRun(t, newWorkspace(t, shapeNewKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
	// The migrated spelling must compile: a struct-literal type error
	// here would be the old shape, and the fallback would report it.
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

var shapeAckSym = upgrade.Symbol{Pkg: "example.com/kit/queue", Name: "Queue", Member: "Ack"}

var shapeQueueOldKit = map[string]string{"queue/queue.go": `package queue

import "context"

type Queue interface {
	Ack(ctx context.Context, jobID string) error
}
`}

var shapeQueueNewKit = map[string]string{"queue/queue.go": `package queue

import "context"

type Job struct{ ID string }

type Queue interface {
	Ack(ctx context.Context, job Job) error
}
`}

var shapeAckNote = &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
	{Symbol: shapeAckSym, Type: regexp.MustCompile(`^func\(ctx context\.Context, jobID string\) error$`)},
}}}

// An app type implementing the interface with the old method shape is a
// hit at its declaration, the way a uses entry on the method would be.
func TestShapesDefOldSignatureHits(t *testing.T) {
	src := `package main

import (
	"context"

	"example.com/kit/queue"
)

type mem struct{}

func (m *mem) Ack(ctx context.Context, jobID string) error { return nil }

var _ queue.Queue = (*mem)(nil)

func main() {}
`
	res := mustRun(t, newWorkspace(t, shapeQueueOldKit, map[string]string{"main.go": src}), shapeAckNote)
	wantHits(t, res, shapeAckNote, hitAt(src, "Ack(ctx", "main.go",
		shapeAckSym.String()+" shape func(ctx context.Context, jobID string) error"))
}

// A ported implementation carries the new shape and is silent.
func TestShapesDefNewSignatureSilent(t *testing.T) {
	src := `package main

import (
	"context"

	"example.com/kit/queue"
)

type mem struct{}

func (m *mem) Ack(ctx context.Context, job queue.Job) error { return nil }

var _ queue.Queue = (*mem)(nil)

func main() {}
`
	res := mustRun(t, newWorkspace(t, shapeQueueNewKit, map[string]string{"main.go": src}), shapeAckNote)
	wantHits(t, res, shapeAckNote)
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}
