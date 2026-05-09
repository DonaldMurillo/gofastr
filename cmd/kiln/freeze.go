package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gofastr/gofastr/kiln/freeze"
	"github.com/gofastr/gofastr/kiln/journal"
)

// runFreeze: kiln freeze --journal .kiln.session.jsonl --dir build/
//
// Reads the on-disk journal, replays it into a session, and emits the
// canonical source artifacts: entities/*.json (loadable by
// framework.EntitiesFromDir) plus world.json (full snapshot, useful
// for an audit trail or reloading into a fresh kiln serve).
//
// This is the "graduate from build mode" step. Once frozen, the
// JSON files can be checked into a regular GoFastr project.
func runFreeze(args []string) int {
	fs := flag.NewFlagSet("kiln freeze", flag.ExitOnError)
	journalPath := fs.String("journal", ".kiln.session.jsonl", "JSONL journal to read")
	dir := fs.String("dir", "build", "target directory for entities/*.json + world.json")
	_ = fs.Parse(args)

	if _, err := os.Stat(*journalPath); err != nil {
		fmt.Fprintf(os.Stderr, "[kiln freeze] journal %s not found: %v\n", *journalPath, err)
		return 1
	}
	jj, err := journal.OpenJSONL(*journalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[kiln freeze] open: %v\n", err)
		return 1
	}
	defer jj.Close()

	sess, err := journal.Replay(jj)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[kiln freeze] replay: %v\n", err)
		return 1
	}

	if err := freeze.Freeze(sess.World, *dir); err != nil {
		fmt.Fprintf(os.Stderr, "[kiln freeze] write: %v\n", err)
		return 1
	}

	entCount := len(sess.World.Entities)
	pageCount := len(sess.World.Pages)
	hookCount := len(sess.World.Hooks)
	routeCount := len(sess.World.Routes)
	fmt.Fprintf(os.Stderr, "[kiln freeze] wrote to %s/\n", *dir)
	fmt.Fprintf(os.Stderr, "  entities: %d  pages: %d  hooks: %d  routes: %d\n",
		entCount, pageCount, hookCount, routeCount)
	fmt.Fprintf(os.Stderr, "  next: gofastr generate (will read entities/*.json and emit Go)\n")
	return 0
}
