// Package uie2e holds the chromedp browser e2e suite that used to live in
// the framework root test package.
//
// Why it is its own package (issue #208): the CI race gate runs the
// framework root under -race, and a full-package -race run timed out
// TestUIE2E_OwnerScope_CrossUserIsolation. That was slowdown, not a data
// race, but a gate that goes red for timing teaches people to ignore it.
// The suite drives a real headless Chrome whose deadlines absorb machine
// load far worse than they absorb race instrumentation, so it lives here
// where the race gate does not reach it.
//
// The tests are external-package by origin (they exercised only exported
// API as framework_test), so this directory holds no production code.
package uie2e
