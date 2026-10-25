package retired

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/retired/names"
)

//go:generate go run ./internal/tablegen -out retired_table.gen.go

// Set is the retired-name set: every class and attribute name a breaking
// note of the migration registry retired, kept only when it falls in the
// kit's namespaces (names.Keep). A nil *Set and a zero Set match
// nothing.
type Set struct {
	classes  map[string]*names.Note // class token -> note (BEM stems included by matchClass)
	attrs    map[string]*names.Note // lowercase attribute name -> note
	attrPre  []attrPrefix           // entries spelled with a trailing "-"
	nonEmpty bool
}

type attrPrefix struct {
	prefix string
	note   *names.Note
}

// Finding is one retired name the check saw in a response.
type Finding struct {
	Kind string // "class" or "attr"
	Name string // the spelling as it appeared (a BEM variant keeps its suffix)
	Note *names.Note
}

// Message renders the finding the way a test failure and a dev console
// warning read it.
func (f Finding) Message() string {
	return fmt.Sprintf("retired markup: %s %q (%s: %s); run gofastr upgrade",
		f.Kind, f.Name, f.Note.Version, f.Note.Change)
}

// New builds a set from the entries names.Keep keeps: the kit's
// namespaces only, first note wins.
func New(entries []names.Entry) *Set {
	set := &Set{}
	for _, e := range names.Keep(entries) {
		set.nonEmpty = true
		if e.Kind == "class" {
			if set.classes == nil {
				set.classes = make(map[string]*names.Note)
			}
			set.classes[e.Name] = e.Note
			continue
		}
		lower := strings.ToLower(e.Name)
		if strings.HasSuffix(lower, "-") {
			// The trailing dash is part of the prefix: data-cui-toggle-
			// retires data-cui-toggle-open, not data-cui-togglex.
			set.attrPre = append(set.attrPre, attrPrefix{prefix: lower, note: e.Note})
			continue
		}
		if set.attrs == nil {
			set.attrs = make(map[string]*names.Note)
		}
		set.attrs[lower] = e.Note
	}
	return set
}

// matchClass resolves one class token to the note that retired it, or
// nil. The registry rule: the token equals the name, or is the name
// wearing a BEM suffix (name--modifier, name__element).
func (s *Set) matchClass(token string) *names.Note {
	if len(s.classes) == 0 {
		return nil
	}
	if note, ok := s.classes[token]; ok {
		return note
	}
	for _, sep := range [...]string{"--", "__"} {
		if i := strings.Index(token, sep); i > 0 {
			if note, ok := s.classes[token[:i]]; ok {
				return note
			}
		}
	}
	return nil
}

// matchAttr resolves one attribute name to the note that retired it, or
// nil. HTML attribute names are case-insensitive; an entry spelled with
// a trailing "-" retires every extension of it.
func (s *Set) matchAttr(name []byte) *names.Note {
	if !s.nonEmpty {
		return nil
	}
	lower := strings.ToLower(string(name))
	if note, ok := s.attrs[lower]; ok {
		return note
	}
	for _, p := range s.attrPre {
		if strings.HasPrefix(lower, p.prefix) {
			return p.note
		}
	}
	return nil
}

// Check scans rendered HTML and returns every distinct retired name it
// carries, in document order, deduped per response. Attribute VALUES
// never match: a kept marker like data-cui-comp="ui-sidebar" is a name
// in a value slot, not a class or an attribute name.
func (s *Set) Check(html []byte) []Finding {
	if !s.nonEmpty {
		return nil
	}
	var out []Finding
	seen := make(map[Finding]bool)
	Scan(html, func(tag []byte, attrs [][]byte, classes [][]byte) {
		for _, tok := range classes {
			note := s.matchClass(string(tok))
			if note == nil {
				continue
			}
			f := Finding{Kind: "class", Name: string(tok), Note: note}
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
		for _, a := range attrs {
			note := s.matchAttr(a)
			if note == nil {
				continue
			}
			f := Finding{Kind: "attr", Name: string(a), Note: note}
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	})
	return out
}

// checkBody reads a recorded response body by its kind. Markup is
// scanned whole. JSON is walked string by string, and only strings
// holding a tag are scanned: a record whose text reads "ui-button" is
// data, while an html-mode signal value is markup. A body sniffed as
// text that is in fact JSON (an RPC that set no Content-Type) is walked
// the same way, since its markup sits behind escaped quotes the tag
// scanner cannot read.
func (s *Set) checkBody(kind bodyKind, body []byte) []Finding {
	if kind == kindMarkup && !looksLikeJSON(body) {
		return s.Check(body)
	}
	var out []Finding
	seen := make(map[Finding]bool)
	dec := json.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		str, ok := tok.(string)
		if !ok || !strings.Contains(str, "<") {
			continue
		}
		for _, f := range s.Check([]byte(str)) {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
}

func looksLikeJSON(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed)
}

var (
	// current is the loaded set; nil means "load on next use", so a
	// test that swaps it out and back can never leave the process with
	// an empty set standing in for an unloaded one.
	current   atomic.Pointer[Set]
	loadMu    sync.Mutex
	loadCount atomic.Int32
)

// Current returns the process-wide retired set, built from the generated
// table (retired_table.gen.go) on first use. Only the scan paths in this
// package call it (test binaries and `gofastr dev`); production never
// does, so the table is never read outside those modes. The table is
// generated from internal/upgrade's migration registry, so a serving
// binary links neither the registry nor its release notes.
func Current() *Set {
	if s := current.Load(); s != nil {
		return s
	}
	loadMu.Lock()
	defer loadMu.Unlock()
	if s := current.Load(); s != nil {
		return s
	}
	loadCount.Add(1)
	set := New(table[:])
	current.Store(set)
	return set
}

// Loads reports how many times the retired set has been built from the
// table. It exists so a test can prove the production path never builds
// it.
func Loads() int { return int(loadCount.Load()) }

// ResetForTest returns the lazy load to its cold state (and restores the
// previous state on cleanup), so a test can observe the load decision
// itself. Test binaries only.
func ResetForTest(t testing.TB) {
	requireTestBinary("ResetForTest")
	t.Helper()
	prev, prevCount := current.Swap(nil), loadCount.Load()
	loadCount.Store(0)
	t.Cleanup(func() {
		current.Store(prev)
		loadCount.Store(prevCount)
	})
}

// UseForTest installs set as the process-wide retired set for the
// duration of t: the way to drive the full reporting path against a
// fixture set. Test binaries only; a parallel test that scans sees
// the fixture set too.
func UseForTest(t testing.TB, set *Set) {
	requireTestBinary("UseForTest")
	t.Helper()
	prev := current.Swap(set)
	t.Cleanup(func() { current.Store(prev) })
}

func requireTestBinary(what string) {
	if !testing.Testing() {
		panic("retired: " + what + " requires a Go test binary")
	}
}
