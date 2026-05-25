package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/docs"
)

// runDocs implements `gofastr docs`. Three modes:
//
//	gofastr docs                    list every topic with one-line summaries
//	gofastr docs <topic>            print the topic's full markdown
//	gofastr docs --grep <term>      search across every topic
//
// The docs are embedded into the binary at build time, so this command
// always speaks for the version of the framework you have installed —
// no GitHub / module-cache fetch needed.
func runDocs(args []string) {
	// --grep / -g
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--grep", "-g":
			term := ""
			if i+1 < len(args) {
				term = args[i+1]
			}
			if term == "" {
				fmt.Fprintln(os.Stderr, "usage: gofastr docs --grep <term>")
				os.Exit(2)
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
		fmt.Fprintln(os.Stderr, err.Error())
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Run `gofastr docs` to see every available topic.")
		os.Exit(1)
	}
	fmt.Print(string(body))
}

func runDocsList() {
	topics, err := docs.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Printf("%s framework docs (%d topics) — `gofastr docs <topic>` to read one\n\n",
		bold("GoFastr"), len(topics))
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
		// Trim long summaries to fit ~120-col terminals.
		const lineCap = 100
		if len(summary) > lineCap {
			summary = summary[:lineCap] + "…"
		}
		fmt.Printf("  %s%s  %s\n",
			green(t.Name),
			strings.Repeat(" ", maxName-len(t.Name)),
			summary,
		)
	}
	fmt.Printf("\n%s `gofastr docs --grep <term>` to search across every topic.\n", dim("→"))
}

func runDocsGrep(term string) {
	hits, err := docs.Search(term)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	if len(hits) == 0 {
		fmt.Printf("No matches for %q.\n", term)
		return
	}
	fmt.Printf("%d match%s for %q:\n\n", len(hits), pluralS(len(hits)), term)
	currentTopic := ""
	for _, h := range hits {
		if h.Topic != currentTopic {
			fmt.Printf("\n%s %s\n", bold("─"), bold(h.Topic))
			currentTopic = h.Topic
		}
		if h.Heading != "" {
			fmt.Printf("  %s%s  %s\n", dim(fmt.Sprintf("L%d", h.Line)), strings.Repeat(" ", 4), dim(h.Heading))
		}
		fmt.Printf("  %s  %s\n", green(fmt.Sprintf("L%d", h.Line)), highlight(h.Excerpt, term))
	}
}

func printDocsHelp() {
	fmt.Print(`gofastr docs — browse framework docs

Usage:
  gofastr docs                  List every topic
  gofastr docs <topic>          Print the topic's markdown body
  gofastr docs --grep <term>    Search across every topic
  gofastr docs --list           List every topic (same as no args)

The docs are embedded at build time — they always describe the framework
version this binary was built against.
`)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// highlight wraps each case-insensitive occurrence of `term` in `s`
// with ANSI bold so the matching word stands out in --grep output.
func highlight(s, term string) string {
	if term == "" {
		return s
	}
	lower := strings.ToLower(s)
	lowerTerm := strings.ToLower(term)
	var b strings.Builder
	i := 0
	for {
		idx := strings.Index(lower[i:], lowerTerm)
		if idx < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+idx])
		b.WriteString(bold(s[i+idx : i+idx+len(term)]))
		i += idx + len(term)
	}
}
