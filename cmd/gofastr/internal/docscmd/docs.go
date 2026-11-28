package docscmd

import (
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/framework/docs"
	"github.com/DonaldMurillo/gofastr/internal/textcut"
)

// RunDocs implements `gofastr docs`: list, read or grep the embedded docs, or
// serve the docs site.
func RunDocs(args []string) {
	if len(args) > 0 && args[0] == "serve" {
		runDocsServe(args[1:])
		return
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		joined := false
		if name, value, ok := strings.Cut(arg, "="); ok && (name == "--grep" || name == "-g") {
			args = append(args[:i:i], append([]string{name, value}, args[i+1:]...)...)
			arg = name
			joined = true
		}
		switch arg {
		case "--grep", "-g":
			term := ""
			if i+1 < len(args) {
				term = args[i+1]
			}
			if !joined && cli.IsFlagToken(term) {
				cli.Fprintf(os.Stderr, "docs: %v\n", cli.NeedsValue(arg))
				cli.OsExit(2)
			}
			if term == "" {
				fmt.Fprintln(os.Stderr, "usage: gofastr docs --grep <term>")
				cli.OsExit(2)
			}
			runDocsGrep(term)
			return
		case "--list", "-l":
			runDocsList()
			return
		case "--help", "-h":
			printDocsHelp()
			return
		}
	}

	if len(args) == 0 {
		runDocsList()
		return
	}

	topic := args[0]
	body, err := docs.Get(topic)
	if err != nil {
		cli.Fprintln(os.Stderr, err.Error())
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Run `gofastr docs` to see every available topic.")
		cli.OsExit(2)
	}
	cli.Print(string(body))
}

func runDocsList() {
	topics, err := docs.List()
	if err != nil {
		cli.Fprintln(os.Stderr, err.Error())
		cli.OsExit(1)
	}
	fmt.Printf("%s framework docs (%d topics); `gofastr docs <topic>` to read one\n\n",
		cli.Bold("GoFastr"), len(topics))
	maxName := 0
	for _, t := range topics {
		if len(t.Name) > maxName {
			maxName = len(t.Name)
		}
	}
	for _, t := range topics {
		summary := t.Summary
		if summary == "" {
			summary = t.Title
		}
		const lineCap = 100
		summary = textcut.Ellipsis(summary, lineCap)
		cli.Printf("  "+cli.Green("%s")+"%s  %s\n",
			t.Name,
			strings.Repeat(" ", maxName-len(t.Name)),
			summary,
		)
	}
	fmt.Printf("\n%s `gofastr docs --grep <term>` to search across every topic.\n", cli.Dim("→"))
}

const grepHitCap = 50

func runDocsGrep(term string) {
	hits, err := docs.SearchWithLimit(term, grepHitCap+1)
	if err != nil {
		cli.Fprintln(os.Stderr, err.Error())
		cli.OsExit(1)
		return
	}
	if len(hits) == 0 {
		cli.Printf("No matches for %q.\n", term)
		return
	}
	if len(hits) > grepHitCap {
		hits = hits[:grepHitCap]
		cli.Printf("More than %d matches for %q; the first %d, most relevant topics first (narrow the term to see the rest):\n\n", grepHitCap, term, grepHitCap)
	} else {
		cli.Printf("%d match%s for %q:\n\n", len(hits), pluralS(len(hits)), term)
	}
	currentTopic := ""
	for _, h := range hits {
		if h.Topic != currentTopic {
			cli.Printf("\n"+cli.Bold("─")+" "+cli.Bold("%s")+"\n", h.Topic)
			currentTopic = h.Topic
		}
		if h.Heading != "" {
			cli.Printf("  "+cli.Dim("L%d")+"    "+cli.Dim("%s")+"\n", h.Line, h.Heading)
		}
		fmt.Print("  " + cli.Green("L"+strconv.Itoa(h.Line)) + "  " + highlight(h.Excerpt, term) + "\n")
	}
}

func printDocsHelp() {
	fmt.Print(`gofastr docs: browse framework docs

Usage:
  gofastr docs                  List every topic
  gofastr docs <topic>          Print the topic's markdown body
  gofastr docs --grep <term>    Search across every topic (or --grep=<term>)
  gofastr docs --list           List every topic (same as no args)
  gofastr docs serve            Browse the docs site on localhost (see --help)

The docs are embedded at build time: they always describe the framework
version this binary was built against. docs serve downloads that
release's site once and caches it.
`)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

func highlight(s, term string) string {
	if term == "" {
		return textsafe.ScrubTerminal(s)
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if end, ok := foldPrefixLen(s[i:], term); ok {
			b.WriteString(cli.Bold(textsafe.ScrubTerminal(s[i : i+end])))
			i += end
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(textsafe.ScrubTerminal(s[i : i+size]))
		i += size
	}
	return b.String()
}

func foldPrefixLen(s, term string) (int, bool) {
	i := 0
	for _, tr := range term {
		if i >= len(s) {
			return 0, false
		}
		sr, size := utf8.DecodeRuneInString(s[i:])
		if unicode.ToLower(sr) != unicode.ToLower(tr) {
			return 0, false
		}
		i += size
	}
	return i, i > 0
}
