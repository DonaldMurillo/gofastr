// Package xwire holds the shared envelope types for the cross-package
// fixtures: the strictness contract belongs to these types, whichever
// package decodes them.
package xwire

// Args is the tool-argument shape kiln/protocol carries: decoded
// strictly on one transport, laxly on another.
type Args struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// OtherArgs gives the aggregate message a second strict type.
type OtherArgs struct {
	Path string `json:"path"`
}
