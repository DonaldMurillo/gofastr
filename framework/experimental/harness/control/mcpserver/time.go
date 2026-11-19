package mcpserver

import "time"

func realTimeNow() time.Time { return time.Now() }

func realKeepaliveTicker() *time.Ticker { return time.NewTicker(15 * time.Second) }
