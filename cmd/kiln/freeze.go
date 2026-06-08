package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
)

// runFreeze: kiln freeze --journal .kiln.session.jsonl --dir build/
//
// Reads the on-disk journal, replays it into a session, and either:
//   - --dir <path>   (default): writes entities/*.json + world.json to disk
//   - --diff         : prints a human-readable summary of what the
//     agent built (entities, pages, hooks, routes,
//     seeds). No files written. Use this to review
//     agent-driven changes before graduating to source.
//
// This is the "graduate from build mode" step. Once frozen, the
// JSON files can be checked into a regular GoFastr project.
func runFreeze(args []string) int {
	fs := flag.NewFlagSet("kiln freeze", flag.ExitOnError)
	journalPath := fs.String("journal", ".kiln.session.jsonl", "JSONL journal to read")
	dir := fs.String("dir", "build", "target directory for entities/*.json + world.json")
	diff := fs.Bool("diff", false, "Print a human-readable summary of cumulative world changes; do not write files")
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

	if *diff {
		printFreezeDiff(os.Stdout, sess)
		return 0
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
	fmt.Fprintf(os.Stderr, "  next: review %s/entities/, then declare these entities in a gofastr.yml blueprint (or in Go) and run `gofastr generate`\n", *dir)
	return 0
}

// printFreezeDiff renders a review-friendly summary of what the journal
// has cumulatively built. Output is stable (alphabetical) so two diffs
// can be compared with diff(1).
func printFreezeDiff(w io.Writer, sess *journal.Session) {
	world := sess.World

	fmt.Fprintf(w, "kiln freeze --diff\n")
	fmt.Fprintf(w, "  app: %q (json: %s)\n", world.App.Name, defaultStr(world.App.JSONCase, "lower_snake"))

	// Entities (sorted by name).
	if len(world.Entities) > 0 {
		fmt.Fprintf(w, "\nentities (%d):\n", len(world.Entities))
		names := make([]string, 0, len(world.Entities))
		for n := range world.Entities {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			ent := world.Entities[n]
			fmt.Fprintf(w, "  + %s [%d fields]", ent.Name, len(ent.Fields))
			if ent.SoftDelete {
				fmt.Fprint(w, " soft_delete")
			}
			if ent.MCP {
				fmt.Fprint(w, " mcp")
			}
			fmt.Fprintln(w)
			for _, f := range ent.Fields {
				flags := []string{}
				if f.Required {
					flags = append(flags, "required")
				}
				if f.Unique {
					flags = append(flags, "unique")
				}
				if f.Default != nil {
					flags = append(flags, fmt.Sprintf("default=%v", f.Default))
				}
				if f.To != "" {
					flags = append(flags, "→"+f.To)
				}
				flagStr := ""
				if len(flags) > 0 {
					flagStr = " (" + joinComma(flags) + ")"
				}
				fmt.Fprintf(w, "      - %s : %s%s\n", f.Name, f.Type, flagStr)
			}
		}
	}

	// Pages.
	if len(world.Pages) > 0 {
		fmt.Fprintf(w, "\npages (%d):\n", len(world.Pages))
		paths := make([]string, 0, len(world.Pages))
		for p := range world.Pages {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			page := world.Pages[p]
			title := page.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(w, "  + %s — %q [root kind: %s]\n", page.Path, title, page.Tree.Kind)
		}
	}

	// Hooks.
	if len(world.Hooks) > 0 {
		fmt.Fprintf(w, "\nhooks (%d):\n", len(world.Hooks))
		for _, h := range world.Hooks {
			fmt.Fprintf(w, "  + %s on %s/%s — %s\n", h.ID, h.Entity, h.When, h.Action.Kind)
		}
	}

	// Routes.
	if len(world.Routes) > 0 {
		fmt.Fprintf(w, "\nroutes (%d):\n", len(world.Routes))
		for _, r := range world.Routes {
			fmt.Fprintf(w, "  + %s %s — %s\n", r.Method, r.Path, r.Action.Kind)
		}
	}

	// Seeds.
	if len(world.Seeds) > 0 {
		fmt.Fprintf(w, "\nseeds (%d):\n", len(world.Seeds))
		for _, s := range world.Seeds {
			fmt.Fprintf(w, "  + %s [%d rows]\n", s.Entity, len(s.Rows))
		}
	}

	// Plans (informational — not part of the produced source, but
	// useful for "did the user actually approve all the destructive
	// ops?" review).
	if len(sess.Plans) > 0 {
		fmt.Fprintf(w, "\nplans (%d):\n", len(sess.Plans))
		ids := make([]string, 0, len(sess.Plans))
		for id := range sess.Plans {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			p := sess.Plans[id]
			status := "proposed"
			if p.Approved {
				status = "approved"
			} else if p.Rejected {
				status = "rejected"
			}
			fmt.Fprintf(w, "  + %s [%s] %d steps", p.PlanID, status, len(p.Steps))
			if len(p.Targets) > 0 {
				fmt.Fprintf(w, ", targets: %d", len(p.Targets))
			}
			fmt.Fprintln(w)
		}
	}

	if len(world.Entities) == 0 && len(world.Pages) == 0 &&
		len(world.Hooks) == 0 && len(world.Routes) == 0 && len(world.Seeds) == 0 {
		fmt.Fprintf(w, "\n(empty world — journal has no world edits)\n")
	}
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
