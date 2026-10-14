// Package procalive answers whether a process id names a running
// process, for code that keeps a pid in a file and has to tell a live
// owner from a stale record: battery/service waiting for its daemon to
// exit, framework/localrun pruning records a crashed run left behind.
package procalive
