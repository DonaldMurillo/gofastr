// Command check-embed fails when an embed.Surface's screen renders a component
// whose ClientJS calls G.serverAction.
//
//	go run ./cmd/check-embed             # scans ./...
//	go run ./cmd/check-embed ./pkg/...   # scans the given patterns
//
// It exits 0 when clean, 1 on a finding or a blocking note, and 2 when a
// package fails to load.
package main

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/check-embed/embedcheck"
)

func main() {
	findings, unresolved, fset, err := embedcheck.CheckAll(os.Args[1:]...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}
	var blocking int
	for _, u := range unresolved {
		fmt.Fprintf(os.Stderr, "%s: %s\n", fset.Position(u.Pos), u.Format())
		if u.Blocking {
			blocking++
		}
	}
	if len(findings) == 0 && blocking == 0 {
		if len(unresolved) > 0 {
			fmt.Printf("  ✓ no embeddable surface registers a server action (%d place(s) not statically followable: see above)\n", len(unresolved))
			return
		}
		fmt.Println("  ✓ no embeddable surface registers a server action")
		return
	}
	if len(findings) == 0 {
		fmt.Fprintf(os.Stderr, "check-embed: %d embed surface path(s) could not be verified: neither this analyzer nor the boot walk can verify a child built inside Render().\n", blocking)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "check-embed: %d embed surface(s) register a server action:\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "%s: %s\n\n", fset.Position(f.Pos), f.Format())
	}
	fmt.Fprintln(os.Stderr, "Fix: use an island RPC, a form POST, or polling: all three work in a frame. "+
		"(G.serverAction posts to the app-global action registry, which has no relationship to any surface.)")
	os.Exit(1)
}
