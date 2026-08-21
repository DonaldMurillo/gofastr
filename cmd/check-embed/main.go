// Command check-embed is the build-time gate for server actions on embeddable
// surfaces. It fails (exit 1) when an embed.Surface's screen renders a
// component whose registered ClientJS calls G.serverAction, the one thing that
// does not work inside a frame, because the action registry is app-global with
// no relationship to any surface.
//
// Usage:
//
//	go run ./cmd/check-embed             # scans ./...
//	go run ./cmd/check-embed ./pkg/...   # scans a specific pattern
//
// Exits 0 when clean, 1 when a surface registers a server action, and 2 on
// infrastructure error (a package failed to parse or type-check). Mirrors the
// check-csp exit-code convention; see framework/uihost/embed_actions.go for the
// boot-time backstop that catches whatever this static pass cannot reach.
package main

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/check-embed/embedcheck"
)

func main() {
	pattern := "./..."
	if len(os.Args) > 1 {
		pattern = os.Args[1]
	}
	findings, unresolved, fset, err := embedcheck.CheckAll(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}
	// Printed before the verdict, and printed even when the verdict is clean:
	// "found nothing" and "could not look" must not read the same.
	//
	// Advisory notes do not change the exit code. The boot walk in
	// framework/uihost covers the shapes they describe. A BLOCKING note does,
	// and must, or this CLI prints a green tick for a tree `gofastr build`
	// refuses to build.
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
		fmt.Fprintf(os.Stderr, "check-embed: %d embed surface path(s) could not be verified: neither this analyzer nor the boot walk can see a child built inside Render() whose type is in another package.\n", blocking)
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
