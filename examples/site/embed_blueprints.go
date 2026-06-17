package main

// Embedded copies of example-app blueprints, shown verbatim on the /examples
// page. These are COMMITTED COPIES, not the source of truth — each has a drift
// test (see embed_blueprints_test.go) that fails the build if it diverges from
// the canonical file under examples/<app>/gofastr.yml. go:embed cannot reach a
// sibling examples/ directory (no ".." traversal), so the copy lives here.

import _ "embed"

//go:embed exampleblueprints/meridian.yml
var meridianBlueprintYAML string
