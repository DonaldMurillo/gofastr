package oz

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// --- df4727141: WSConfig SendBuffer, pre-fix ------------------------

type WSConfig struct {
	SendBuffer   int
	WriteTimeout time.Duration
	KeepAlive    time.Duration
	Workers      uint
}

func Upgrade(cfg WSConfig) chan []byte {
	sendBuf := cfg.SendBuffer
	if sendBuf == 0 { // want `sendBuf == 0 takes the default but a negative sendBuf passes through to make\(\)`
		sendBuf = 32
	}
	return make(chan []byte, sendBuf)
}

// The fix spelling: <= 0 takes the default for both.
func UpgradeFixed(cfg WSConfig) chan []byte {
	sendBuf := cfg.SendBuffer
	if sendBuf <= 0 {
		sendBuf = 32
	}
	return make(chan []byte, sendBuf)
}

// A field subject reaching a deadline Add.
func writeDeadline(cfg WSConfig, now time.Time) time.Time {
	if cfg.WriteTimeout == 0 { // want `cfg.WriteTimeout == 0 takes the default but a negative cfg.WriteTimeout passes through to time.Time.Add`
		cfg.WriteTimeout = 10 * time.Second
	}
	return now.Add(cfg.WriteTimeout)
}

// A parameter reaching context.WithTimeout and a timer.
func drain(ctx context.Context, timeout time.Duration) {
	if timeout == 0 { // want `timeout == 0 takes the default but a negative timeout passes through to context.WithTimeout`
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_ = ctx
}

// A LIMIT bound.
func page(db *sql.DB, limit int) {
	if limit == 0 { // want `limit == 0 takes the default but a negative limit passes through to a SQL LIMIT`
		limit = 50
	}
	_, _ = db.Query("SELECT id FROM jobs ORDER BY id LIMIT ?", limit)
}

// Quiet: negative is handled elsewhere in the function ("negative =
// disable" keepalive).
func keepalive(cfg WSConfig) {
	if cfg.KeepAlive == 0 {
		cfg.KeepAlive = 30 * time.Second
	}
	if cfg.KeepAlive < 0 {
		return
	}
	_ = time.NewTicker(cfg.KeepAlive)
}

// Quiet: an unsigned subject has no negative.
func pool(cfg WSConfig) chan struct{} {
	n := cfg.Workers
	if n == 0 {
		n = 4
	}
	return make(chan struct{}, n)
}

// Quiet: no sink a negative breaks.
func retries(n int) int {
	if n == 0 {
		n = 3
	}
	return n * 2
}

// Near-miss: an x == 0 branch that returns instead of defaulting.
func nothing(n int) chan int {
	if n == 0 {
		return nil
	}
	return make(chan int, n)
}

// Near-miss: the sink takes a different value than the defaulted one.
func other(n, m int) chan int {
	if n == 0 {
		n = 8
	}
	_ = n
	return make(chan int, m)
}

// Quiet: the source of the local is sign-checked before the copy
// (evals codex.go).
type invocation struct{ Timeout time.Duration }

func run(ctx context.Context, inv invocation) {
	if inv.Timeout < 0 {
		return
	}
	timeout := inv.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_ = ctx
}

// Fires: the copy is not checked and neither is its source.
func runUnchecked(ctx context.Context, inv invocation) {
	timeout := inv.Timeout
	if timeout == 0 { // want `timeout == 0 takes the default`
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_ = ctx
}

// Fires: the sign check sits AFTER the sink and only logs — the
// reaching path to the sink still carries the negative.
func pageLogged(db *sql.DB, limit int) {
	if limit == 0 { // want `limit == 0 takes the default but a negative limit passes through to a SQL LIMIT`
		limit = 50
	}
	_, _ = db.Query("SELECT id FROM jobs ORDER BY id LIMIT ?", limit)
	if limit < 0 {
		log.Printf("negative limit")
	}
}

// Quiet: the sign check precedes the sink, even after the default.
func pageCheckedLate(db *sql.DB, limit int) {
	if limit == 0 {
		limit = 50
	}
	if limit < 0 {
		return
	}
	_, _ = db.Query("SELECT id FROM jobs ORDER BY id LIMIT ?", limit)
}
