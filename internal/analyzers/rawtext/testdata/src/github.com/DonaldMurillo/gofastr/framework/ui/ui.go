// Package ui is the fixture stub of framework/ui.
package ui

import "context"

// TagConfig says its dismiss label through Ctx.
type TagConfig struct {
	Ctx   context.Context
	Label string
}

type ButtonConfig struct {
	Label       string
	SubmitLabel string
	Class       string
}

type NumberFieldConfig struct {
	Label string
	Help  string
}
