package contracts

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Example is a bad/good pair shown under a rule. Both halves are
// required: "don't do this" without "do this instead" is the failure mode
// that makes linters feel adversarial, and it is exactly the half an
// agent needs in order to produce a fix on the first attempt.
type Example struct {
	// Caption is the one-line framing ("a POST route with no access
	// declaration"). Optional.
	Caption string `json:"caption,omitempty"`
	// Bad is the code that trips the rule.
	Bad string `json:"bad"`
	// Good is the code that satisfies it.
	Good string `json:"good"`
}

// Rule is the documentation half of a contract: everything a human or an
// agent needs to understand a diagnostic without opening the source of
// the analyzer that produced it.
//
// Every field except Autofix and Examples is mandatory, and
// [RegisterRules] enforces that. A rule with an empty Fix is a rule that
// will be suppressed rather than fixed.
type Rule struct {
	// ID is the stable identifier — "GOFASTR1002". Assigned from the
	// capability's number block (see catalog.go) and never reused, so a
	// suppression written today keeps meaning the same thing.
	ID string `json:"id"`
	// Slug is the human-readable name — "routing/missing-auth". Accepted
	// anywhere an ID is, because `//gofastr:allow(routing/missing-auth)`
	// reads better in a diff than a number.
	Slug string `json:"slug"`
	// Title is the short noun phrase shown as the finding's headline.
	Title string `json:"title"`
	// Capability is the area this rule belongs to.
	Capability Capability `json:"capability"`
	// Severity is the default severity. Config may lower it; nothing
	// raises it.
	Severity Severity `json:"severity"`
	// Summary is one sentence stating what was detected.
	Summary string `json:"summary"`
	// Why explains the consequence — what breaks, for whom, when. This is
	// the field that turns a lint error into a lesson.
	Why string `json:"why"`
	// Fix is the concrete remedy, in imperative voice, naming the exact
	// API or file to reach for.
	Fix string `json:"fix"`
	// Examples are bad/good pairs. Optional but strongly encouraged.
	Examples []Example `json:"examples,omitempty"`
	// Doc is the `gofastr docs` topic that covers this rule in depth
	// (e.g. "reactivity"). Rendered as a URL by DocURL.
	Doc string `json:"doc"`
	// Autofix reports whether an analyzer can produce a mechanical edit
	// for this rule. Advisory: a rule may be marked autofixable and still
	// decline to fix a particular instance it cannot rewrite safely.
	Autofix bool `json:"autofix"`
}

// docBaseURL is where the embedded docs are published. Diagnostics carry
// a resolvable link even when the reader is nowhere near a checkout.
const docBaseURL = "https://gofastr.dev/docs/"

// DocURL is the published location of the rule's doc topic.
func (r Rule) DocURL() string {
	if r.Doc == "" {
		return ""
	}
	return docBaseURL + r.Doc
}

// DocCommand is the offline equivalent of DocURL — the docs are embedded
// in the binary, so an agent with no network still has the full text.
func (r Rule) DocCommand() string {
	if r.Doc == "" {
		return ""
	}
	return "gofastr docs " + r.Doc
}

var (
	ruleMu      sync.RWMutex
	rulesByID   = map[string]Rule{}
	rulesBySlug = map[string]string{} // slug → ID

	// An ID is an uppercase prefix plus a number: GOFASTR1002, ACME101.
	// The prefix is the namespace — GOFASTR belongs to the built-in
	// catalog and its per-capability number blocks; a project registering
	// its own rules picks its own prefix, which is what makes a custom ID
	// unable to collide with a future catalog entry.
	reRuleID   = regexp.MustCompile(`^[A-Z]{2,12}[0-9]{3,4}$`)
	reRuleSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*/[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// RegisterRules adds rules to the process-wide catalog, validating each
// one. It panics on a malformed or duplicate rule: the catalog is
// compiled-in data, so a violation is a programming error that should
// never reach a user's terminal, and failing at init is how it stays that
// way.
func RegisterRules(rules ...Rule) {
	ruleMu.Lock()
	defer ruleMu.Unlock()
	for _, r := range rules {
		if err := validateRule(r); err != nil {
			panic("contracts: " + err.Error())
		}
		if _, dup := rulesByID[r.ID]; dup {
			panic(fmt.Sprintf("contracts: duplicate rule ID %s", r.ID))
		}
		if prior, dup := rulesBySlug[r.Slug]; dup {
			panic(fmt.Sprintf("contracts: duplicate rule slug %q (already used by %s)", r.Slug, prior))
		}
		rulesByID[r.ID] = r
		rulesBySlug[r.Slug] = r.ID
	}
}

func validateRule(r Rule) error {
	if !reRuleID.MatchString(r.ID) {
		return fmt.Errorf("rule ID %q must be an uppercase prefix plus a number, like GOFASTR1002 or ACME101", r.ID)
	}
	if !reRuleSlug.MatchString(r.Slug) {
		return fmt.Errorf("rule %s: slug %q must be kebab-case `capability/name`", r.ID, r.Slug)
	}
	if !r.Capability.Valid() {
		return fmt.Errorf("rule %s: unknown capability %q", r.ID, r.Capability)
	}
	if got := strings.SplitN(r.Slug, "/", 2)[0]; got != string(r.Capability) {
		return fmt.Errorf("rule %s: slug prefix %q does not match capability %q", r.ID, got, r.Capability)
	}
	// Block discipline is a GOFASTR-namespace rule: the catalog assigns
	// each capability a number range so IDs stay stable and greppable.
	// It applies to GOFASTR + digits EXACTLY — a prefix that merely
	// starts with the letters (GOFASTRA123) is somebody's custom prefix,
	// and routing it here panicked a host app at init with a message
	// about numbers.
	if rest := strings.TrimPrefix(r.ID, "GOFASTR"); rest != r.ID {
		if n, err := strconv.Atoi(rest); err == nil {
			if block, ok := capabilityBlock(r.Capability); ok && (n < block || n >= block+100) {
				return fmt.Errorf("rule %s: capability %q owns the %d–%d block", r.ID, r.Capability, block, block+99)
			}
		}
	}
	if r.Severity == SeverityOff {
		return fmt.Errorf("rule %s: a rule may not declare severity off — omit it from the catalog instead", r.ID)
	}
	for field, val := range map[string]string{
		"Title": r.Title, "Summary": r.Summary, "Why": r.Why, "Fix": r.Fix, "Doc": r.Doc,
	} {
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("rule %s: %s is required", r.ID, field)
		}
	}
	// A rule with no example is a rule the reader has to infer from prose.
	// Twenty of the original forty-seven shipped that way, and each one
	// was a worse answer from `--explain` and `contracts_explain` than the
	// same rule with three lines of before/after. Required, not encouraged.
	if len(r.Examples) == 0 {
		return fmt.Errorf("rule %s: at least one bad/good Example is required — a rule the reader cannot see is a rule they will not apply", r.ID)
	}
	for i, ex := range r.Examples {
		if strings.TrimSpace(ex.Bad) == "" || strings.TrimSpace(ex.Good) == "" {
			return fmt.Errorf("rule %s: example %d needs both a Bad and a Good half", r.ID, i)
		}
	}
	return nil
}

// LookupRule resolves a rule by ID or slug, case-insensitively on the ID.
func LookupRule(idOrSlug string) (Rule, bool) {
	ruleMu.RLock()
	defer ruleMu.RUnlock()
	return lookupRuleLocked(idOrSlug)
}

func lookupRuleLocked(idOrSlug string) (Rule, bool) {
	key := strings.TrimSpace(idOrSlug)
	if r, ok := rulesByID[strings.ToUpper(key)]; ok {
		return r, true
	}
	if id, ok := rulesBySlug[strings.ToLower(key)]; ok {
		return rulesByID[id], true
	}
	return Rule{}, false
}

// AllRules returns the whole catalog sorted by capability order then ID —
// the order `gofastr verify --list` prints and the MCP catalog returns.
func AllRules() []Rule {
	ruleMu.RLock()
	defer ruleMu.RUnlock()
	out := make([]Rule, 0, len(rulesByID))
	for _, r := range rulesByID {
		out = append(out, r)
	}
	sortRules(out)
	return out
}

// RulesFor returns every catalog rule in the given capability.
func RulesFor(c Capability) []Rule {
	ruleMu.RLock()
	defer ruleMu.RUnlock()
	var out []Rule
	for _, r := range rulesByID {
		if r.Capability == c {
			out = append(out, r)
		}
	}
	sortRules(out)
	return out
}

func sortRules(rs []Rule) {
	sort.Slice(rs, func(i, j int) bool {
		if oi, oj := rs[i].Capability.Order(), rs[j].Capability.Order(); oi != oj {
			return oi < oj
		}
		return rs[i].ID < rs[j].ID
	})
}

// SuggestRules returns catalog entries whose ID or slug is close to the
// given string — the "did you mean" behind an unknown-rule config error.
func SuggestRules(idOrSlug string) []string {
	needle := strings.ToLower(strings.TrimSpace(idOrSlug))
	if needle == "" {
		return nil
	}
	ruleMu.RLock()
	defer ruleMu.RUnlock()
	var out []string
	for id, r := range rulesByID {
		if strings.Contains(strings.ToLower(id), needle) || strings.Contains(r.Slug, needle) {
			out = append(out, fmt.Sprintf("%s (%s)", id, r.Slug))
		}
	}
	// Substring matching cannot help with a mistyped ID, which is the most
	// likely thing to mistype: IDs are copied out of a report by hand, and
	// `GOFASTR1oo2` shares no substring with `GOFASTR1002`. Fall back to
	// edit distance so the one case with no partial-word to match on still
	// gets an answer.
	if len(out) == 0 {
		// Already ranked by distance — do NOT re-sort alphabetically, or
		// a close match can be pushed past the cap by distant ones.
		out = suggestByDistance(needle)
	} else {
		sort.Strings(out)
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// suggestByDistance ranks catalog entries by edit distance from the
// needle. The budget scales with length so a short slug does not match
// half the catalog, and caps at three so a wild guess gets nothing rather
// than a misleading list. Caller holds ruleMu.
func suggestByDistance(needle string) []string {
	budget := len(needle) / 4
	if budget < 1 {
		budget = 1
	}
	if budget > 3 {
		budget = 3
	}
	type scored struct {
		text string
		dist int
	}
	var hits []scored
	for id, r := range rulesByID {
		best := editDistance(needle, strings.ToLower(id))
		if d := editDistance(needle, r.Slug); d < best {
			best = d
		}
		if best <= budget {
			hits = append(hits, scored{fmt.Sprintf("%s (%s)", id, r.Slug), best})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].dist != hits[j].dist {
			return hits[i].dist < hits[j].dist
		}
		return hits[i].text < hits[j].text
	})
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.text)
	}
	return out
}

// editDistance is Levenshtein, two rows rather than a full matrix.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}
