// Package static renders a UIHost's routes to a directory of HTML and
// assets that any static host can serve. Builder.Build writes the export,
// Builder.Watch rebuilds it on change, and Walk yields the pages an export
// would write.
package static
