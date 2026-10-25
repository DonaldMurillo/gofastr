// Package treegate holds the framework/ui gates that read outside the
// package: the removed-alias token scan over every tree that ships the
// design system or uses it, and the component inventory checks against
// doc.go and framework/docs/content/ui-new-components.md. They live apart
// from framework/ui so that a change in one of the trees they read
// selects this small package, not framework/ui's browser suite.
package treegate
