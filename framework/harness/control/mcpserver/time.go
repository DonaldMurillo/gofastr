package mcpserver

import "time"

func realTimeNow() time.Time { return time.Now() }

func realKeepaliveTicker() *time.Ticker { return time.NewTicker(15 * time.Second) }

// JSON marshaling helper kept here to avoid pulling extra imports
// into the busy server.go file.
type _ struct{}
