package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

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
				osExit(2)
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
		osExit(1)
	}
	fmt.Print(string(body))
}

func runDocsList() {
	topics, err := docs.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		osExit(1)
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
			// Back off to a rune boundary — a byte slice can land mid-rune
			// and emit invalid UTF-8 to the terminal.
			cut := lineCap
			for cut > 0 && !utf8.RuneStart(summary[cut]) {
				cut--
			}
			summary = summary[:cut] + "…"
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
		osExit(1)
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
//
// It scans s directly, rune by rune, rather than locating the match in
// strings.ToLower(s) and slicing the original. strings.ToLower is not
// length-preserving — U+0130 'İ' lowers to a single-byte 'i', shrinking
// 2 bytes to 1 — so the two strings' byte offsets diverge and the old
// `s[i+idx : i+idx+len(term)]` either sliced mid-rune (emitting invalid
// UTF-8) or ran past the end. `gofastr docs --grep 'entİty'` panicked
// with "slice bounds out of range".
func highlight(s, term string) string {
	if term == "" {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if end, ok := foldPrefixLen(s[i:], term); ok {
			b.WriteString(bold(s[i : i+end]))
			i += end
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

// foldPrefixLen reports the byte length of s's leading run of runes that
// case-folds equal to term, and whether such a prefix exists. Because it
// advances by whole runes in both strings, the returned length is always a
// valid slice bound into s.
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
