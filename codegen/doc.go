// Package codegen provides YAML-driven code generation primitives for GoFastr.
//
// Typical CLI-style usage discovers or loads a Config, registers built-in
// generators and extension commands on a Registry, runs generation into a
// FileSet, then writes files with WriteFiles. Embedders can register
// in-process generators and extensions directly and may skip command extension
// registration entirely.
package codegen
