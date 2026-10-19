package main

// Embedded copies of example app specs, shown verbatim on the /examples
// page. These are COMMITTED COPIES, not the source of truth, each has a drift
// test (see embed_appspecs_test.go) that fails the build if it diverges from
// the canonical file under examples/<app>/gofastr.yml. go:embed cannot reach a
// sibling examples/ directory (no ".." traversal), so the copy lives here.

import _ "embed"

//go:embed exampleappspecs/meridian.yml
var meridianAppSpecYAML string
