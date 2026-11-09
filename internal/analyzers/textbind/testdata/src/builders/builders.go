// Package builders: request text through a strings.Builder, the
// residual the analyzer's own doc named (strings.ToLower's body is a
// Builder; app code builds SQL bind values the same way).
package builders

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
)

var db *sql.DB

func handle(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("prefix:")
	b.WriteString(r.FormValue("q"))
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
}

// A write through the byte-slice form carries the same bytes.
func byteWrite(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.Write([]byte(r.Header.Get("X-Note")))
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
}

// Reset empties the builder: only what is written after it counts.
func reset(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(r.FormValue("q"))
	b.Reset()
	b.WriteString("clean")
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String())
}

// A read BEFORE the reset still sees the writes before it: the reset
// later in the function must not erase them (2026-11-08 review
// finding on the first cut of this arm).
func readBeforeReset(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(r.FormValue("q"))
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
	b.Reset()
	b.WriteString("clean")
	_, _ = w, r
}

// A Reset inside a conditional may never run: the request text may
// still be in the builder at the read, so the write still counts
// (the review gate's second finding, on the position-only cut).
func conditionalReset(w http.ResponseWriter, r *http.Request, cond bool) {
	var b strings.Builder
	b.WriteString(r.FormValue("q"))
	if cond {
		b.Reset()
	}
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
	_, _ = w, r
}

// A Reset in one arm does not clear the builder for a read in the
// sibling arm: on that path the reset never ran (review round 3).
func siblingBranchReset(w http.ResponseWriter, r *http.Request, cond bool) {
	var b strings.Builder
	b.WriteString(r.FormValue("q"))
	if cond {
		b.Reset()
	} else {
		db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
	}
	_, _ = w, r
}

// A Reset inside a loop the loop may run zero times does not clear
// the writes that precede it (review round 3).
func zeroTripLoopReset(w http.ResponseWriter, r *http.Request, items []string) {
	var b strings.Builder
	b.WriteString(r.FormValue("q"))
	for range items {
		b.Reset()
	}
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
	_, _ = w, r
}

func clean() {
	var b strings.Builder
	b.WriteString("static")
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String())
}

// A builder stored through a pointer local builds the same way.
func pointerLocal(w http.ResponseWriter, r *http.Request) {
	b := &strings.Builder{}
	b.WriteString(r.PathValue("slug"))
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
}

// A reset in another switch arm never runs on the binding path.
func switchSiblingReset(r *http.Request, arm int) {
	var b strings.Builder
	b.WriteString(r.FormValue("q"))
	switch arm {
	case 0:
		b.Reset()
	default:
		db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
	}
}

// Sharing a loop does not make a conditional reset unconditional.
func conditionalLoopReset(r *http.Request, items []bool) {
	var b strings.Builder
	for _, reset := range items {
		b.WriteString(r.FormValue("q"))
		if reset {
			b.Reset()
		}
		db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
	}
}

func sameSwitchArmReset(r *http.Request, arm int) {
	var b strings.Builder
	b.WriteString(r.FormValue("q"))
	switch arm {
	case 0:
		b.Reset()
		db.Exec("INSERT INTO b (v) VALUES ($1)", b.String())
	}
}

func indexedByte(r *http.Request) {
	var b strings.Builder
	s := r.FormValue("q")
	if len(s) > 0 {
		b.WriteByte(s[0])
	}
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
}

func runeLoop(r *http.Request) {
	var b strings.Builder
	for _, c := range r.FormValue("q") {
		b.WriteRune(c)
	}
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
}

func formatted(r *http.Request) {
	var b strings.Builder
	fmt.Fprintf(&b, "prefix:%s", r.FormValue("q"))
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String()) // want `request text reaches a SQL bind`
}

// Loop offsets are integers, not the request's bytes.
func loopOffsets(r *http.Request) {
	var b strings.Builder
	for i := range r.FormValue("q") {
		b.WriteRune(rune(i))
	}
	db.Exec("INSERT INTO b (v) VALUES ($1)", b.String())
}
