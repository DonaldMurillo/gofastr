// Package elements provides semantic, ADA-compliant HTML element primitives
// for the GoFastr core-ui framework.
//
// Every element function returns a [render.HTML] value and uses the core
// render package's Tag and VoidTag builders to produce well-formed markup.
// ARIA landmarks and roles are applied automatically where appropriate.
//
// # Usage
//
// Elements accept an Attrs map (which may be nil) and variadic children:
//
//	heading := elements.Heading(2, elements.Attrs{"class": "title"},
//	    render.Text("Welcome"))
//	// <h2 class="title" id="heading-welcome">Welcome</h2>
package elements
