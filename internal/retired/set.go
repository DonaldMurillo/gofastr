package retired

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// Set is the retired-name set derived from the migration registry: the
// union of every breaking note's strings.classes, css.classes and
// strings.attrs that fall in the kit's namespaces (see kitClass). A nil
// *Set and a zero Set match nothing.
type Set struct {
	classes  map[string]*upgrade.Note // class token -> note (BEM stems included by matchClass)
	attrs    map[string]*upgrade.Note // lowercase attribute name -> note
	attrPre  []attrPrefix             // entries spelled with a trailing "-"
	nonEmpty bool
}

type attrPrefix struct {
	prefix string
	note   *upgrade.Note
}

// Finding is one retired name the check saw in a response.
type Finding struct {
	Kind string // "class" or "attr"
	Name string // the spelling as it appeared (a BEM variant keeps its suffix)
	Note *upgrade.Note
}

// Message renders the finding the way a test failure and a dev console
// warning read it.
func (f Finding) Message() string {
	return fmt.Sprintf("retired markup: %s %q (%s: %s); run gofastr upgrade",
		f.Kind, f.Name, f.Note.Version, f.Note.Change)
}

// FromRegistry builds the retired set. Releases are walked in order and
// the first note naming a class wins, so a name retired in one release
// and mentioned again later keeps its original note. Non-breaking notes
// contribute nothing: their spellings are not retired.
func FromRegistry(reg *upgrade.Registry) *Set {
	set := &Set{}
	if reg == nil {
		return set
	}
	for i := range reg.Releases {
		for _, note := range reg.Releases[i].Notes {
			if !note.Breaking {
				continue
			}
			for _, name := range note.Find.Strings.Classes {
				set.addClass(name, note)
			}
			for _, name := range note.Find.CSS.Classes {
				set.addClass(name, note)
			}
			for _, name := range note.Find.Strings.Attrs {
				set.addAttr(name, note)
			}
		}
	}
	return set
}

// kitClass and kitAttr are the namespaces the kit owns. A breaking
// note can retire a generic name the kit used to emit (a "card" class,
// a data-placeholder attribute), but an app may own that name too, so
// only names in these namespaces are read as retired in rendered
// markup; the source scan still reports the rest.
func kitClass(name string) bool {
	return strings.HasPrefix(name, "ui-") || strings.HasPrefix(name, "fui-")
}

func kitAttr(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "data-fui-") || strings.HasPrefix(lower, "data-hui-")
}

func (s *Set) addClass(name string, note *upgrade.Note) {
	if !kitClass(name) {
		return
	}
	if s.classes == nil {
		s.classes = make(map[string]*upgrade.Note)
	}
	if _, dup := s.classes[name]; !dup {
		s.classes[name] = note
		s.nonEmpty = true
	}
}

func (s *Set) addAttr(name string, note *upgrade.Note) {
	if !kitAttr(name) {
		return
	}
	if strings.HasSuffix(name, "-") && len(name) > 1 {
		// The trailing dash is part of the prefix: data-fui-toggle-
		// retires data-fui-toggle-open, not data-fui-togglex.
		s.addAttrPrefixLocked(name, note)
		return
	}
	if s.attrs == nil {
		s.attrs = make(map[string]*upgrade.Note)
	}
	lower := strings.ToLower(name)
	if _, dup := s.attrs[lower]; !dup {
		s.attrs[lower] = note
		s.nonEmpty = true
	}
}

func (s *Set) addAttrPrefixLocked(prefix string, note *upgrade.Note) {
	prefix = strings.ToLower(prefix)
	for _, p := range s.attrPre {
		if p.prefix == prefix {
			return
		}
	}
	s.attrPre = append(s.attrPre, attrPrefix{prefix: prefix, note: note})
	s.nonEmpty = true
}

// matchClass resolves one class token to the note that retired it, or
// nil. The registry rule: the token equals the name, or is the name
// wearing a BEM suffix (name--modifier, name__element).
func (s *Set) matchClass(token string) *upgrade.Note {
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
func (s *Set) matchAttr(name []byte) *upgrade.Note {
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
// never match: a kept marker like data-fui-comp="ui-sidebar" is a name
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
	emptySet  Set
)

// Current returns the process-wide retired set, loading the embedded
// migration registry on first use. Only the scan paths in this package
// call it (test binaries and `gofastr dev`); production never does, so
// the registry is never parsed outside those modes. A registry that
// fails to load disables the check for the process — the check is a
// migration aid, not a serving dependency.
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
	set := &emptySet
	if reg, err := upgrade.Load(); err != nil {
		slog.Error("retired: migration registry failed to load; markup check disabled", "err", err)
	} else {
		set = FromRegistry(reg)
	}
	current.Store(set)
	return set
}

// Loads reports how many times the migration registry has been loaded.
// It exists so a test can prove the production path never triggers one.
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
// fixture registry. Test binaries only; a parallel test that scans sees
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
