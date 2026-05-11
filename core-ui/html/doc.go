// Package html provides semantic, ADA-compliant HTML element primitives
// for the GoFastr core-ui framework.
//
// The package maps 1:1 to HTML tags: every exported function produces a
// single element (Div, Button, Heading, Form, Table…). Higher-level
// patterns that compose multiple elements (accordion, pagination, tabs,
// breadcrumbs, etc.) live in [core-ui/patterns]; opinionated semantic
// components live in [framework/ui].
//
// Every function returns a [render.HTML] value and uses the core render
// package's Tag and VoidTag builders to produce well-formed markup.
// ARIA landmarks and roles are applied automatically where appropriate.
//
// # Usage
//
// Elements accept an Attrs map (which may be nil) and variadic children:
//
//	heading := html.Heading(2, html.Attrs{"class": "title"},
//	    render.Text("Welcome"))
//	// <h2 class="title" id="heading-welcome">Welcome</h2>
package html
