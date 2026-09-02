// Package attribute is a minimal stand-in for
// go.opentelemetry.io/otel/attribute: analysistest fixtures load in
// GOPATH mode, where module dependencies are unavailable. The names and
// signatures the controlbytes rule matches on are preserved exactly;
// only the package is local.
package attribute

type Attr struct{ K, V string }

func String(key, value string) Attr { return Attr{K: key, V: value} }
