package contracts

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Example is a bad/good pair shown under a rule. Both halves are required.
type Example struct {
	// Caption is an optional one-line framing.
	Caption string `json:"caption,omitempty"`
	// Bad is the code that trips the rule.
	Bad string `json:"bad"`
	// Good is the code that satisfies it.
	Good string `json:"good"`
}

// Rule is the documentation half of a contract. [RegisterRules] requires
// every field except Caption and Autofix, and at least one Example.
type Rule struct {
	// ID is the stable identifier, "GOFASTR1002", from the capability's
	// number block and never reused.
	ID string `json:"id"`
	// Slug is the human-readable name, "routing/missing-auth", accepted
	// anywhere an ID is.
	Slug string `json:"slug"`
	// Title is the short noun phrase shown as the finding's headline.
	Title string `json:"title"`
	// Capability is the area this rule belongs to.
	Capability Capability `json:"capability"`
	// Severity is the default severity; config may move it either way.
	Severity Severity `json:"severity"`
	// Summary is one sentence stating what was detected.
	Summary string `json:"summary"`
	// Why explains the consequence: what breaks, for whom, when.
	Why string `json:"why"`
	// Fix is the concrete remedy, in imperative voice, naming the exact
	// API or file to reach for.
	Fix string `json:"fix"`
	// Examples are bad/good pairs; at least one is required.
	Examples []Example `json:"examples,omitempty"`
	// Doc is the `gofastr docs` topic covering this rule, e.g. "reactivity".
	Doc string `json:"doc"`
	// Autofix reports whether an analyzer can produce a mechanical edit
	// for this rule; it may still decline a particular instance.
	Autofix bool `json:"autofix"`
}

const docBaseURL = "https://donaldmurillo.github.io/gofastr/docs/"

// DocURL is the published location of the rule's doc topic.
func (r Rule) DocURL() string {
	if r.Doc == "" {
		return ""
	}
	return docBaseURL + r.Doc
}

// DocCommand is the offline equivalent of DocURL.
func (r Rule) DocCommand() string {
	if r.Doc == "" {
		return ""
	}
	return "gofastr docs " + r.Doc
}

var (
	ruleMu      sync.RWMutex
	rulesByID   = map[string]Rule{}
	rulesBySlug = map[string]string{}

	reRuleID   = regexp.MustCompile(`^[A-Z]{2,12}[0-9]{3,4}$`)
	reRuleSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*/[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// RegisterRules adds rules to the process-wide catalog, validating each
// one. It panics on a malformed or duplicate rule.
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
	if rest := strings.TrimPrefix(r.ID, "GOFASTR"); rest != r.ID {
		if n, err := strconv.Atoi(rest); err == nil {
			if block, ok := capabilityBlock(r.Capability); ok && (n < block || n >= block+100) {
				return fmt.Errorf("rule %s: capability %q owns the %d–%d block", r.ID, r.Capability, block, block+99)
			}
		}
	}
	if r.Severity == SeverityOff {
		return fmt.Errorf("rule %s: a rule may not declare severity off: omit it from the catalog instead", r.ID)
	}
	for _, entry := range []struct {
		key string
		val string
	}{
		{"Title", r.Title},
		{"Summary", r.Summary},
		{"Why", r.Why},
		{"Fix", r.Fix},
		{"Doc", r.Doc},
	} {
		field, val := entry.key, entry.val
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("rule %s: %s is required", r.ID, field)
		}
	}
	if len(r.Examples) == 0 {
		return fmt.Errorf("rule %s: at least one bad/good Example is required: a rule the reader cannot see is a rule they will not apply", r.ID)
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
	if strings.ContainsFunc(key, func(r rune) bool { return r >= 0x80 }) {
		return Rule{}, false
	}
	if r, ok := rulesByID[strings.ToUpper(key)]; ok {
		return r, true
	}
	if id, ok := rulesBySlug[strings.ToLower(key)]; ok {
		return rulesByID[id], true
	}
	return Rule{}, false
}

// AllRules returns the whole catalog sorted by capability order then ID:
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
// given string, the "did you mean" behind an unknown-rule config error.
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
	if len(out) == 0 {
		out = suggestByDistance(needle)
	} else {
		sort.Strings(out)
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

const maxSuggestNeedle = 64

// suggestByDistance ranks catalog entries by edit distance from the
// needle, within a budget of len/4 clamped to 1..3. Caller holds ruleMu.
func suggestByDistance(needle string) []string {
	if len(needle) > maxSuggestNeedle {
		needle = needle[:maxSuggestNeedle]
	}
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
