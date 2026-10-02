// Package route is the analysistest stand-in for the design's
// core-ui/route package (the route snapshot carrier). It does not
// exist in the spike yet; the analyzer carries the matcher so the lint
// covers route.From the day the package lands.
package route

import "context"

// State is the route snapshot.
type State struct {
	Path string
}

// From reads the route state off the context.
func From(ctx context.Context) (State, bool) {
	return State{}, false
}
