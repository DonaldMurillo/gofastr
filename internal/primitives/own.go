package primitives

import (
	"path"
	"slices"
	"strings"
)

// Chain returns the primitives that own file, outermost first: the
// deepest primitive whose code covers it, preceded by its ancestors.
func (c *Registry) Chain(file string) []*Primitive {
	var owner *Primitive
	for _, p := range c.Primitives {
		if p.Owns(file) && (owner == nil || depth(p.ID) > depth(owner.ID)) {
			owner = p
		}
	}
	var chain []*Primitive
	for p := owner; p != nil; p = c.Parent(p) {
		chain = append([]*Primitive{p}, chain...)
	}
	return chain
}

func (p *Primitive) Owns(file string) bool { return matchAny(p.Code, file) }

func matchAny(refs []Ref, file string) bool {
	return slices.ContainsFunc(refs, func(ref Ref) bool { return matchesPath(ref.Value, file) })
}

// matchesPath treats an entry ending in a slash as a directory tree.
func matchesPath(entry, file string) bool {
	if strings.HasSuffix(entry, "/") {
		return strings.HasPrefix(file, entry)
	}
	ok, _ := path.Match(entry, file)
	return ok
}

// Applied is an invariant that governs a file its primitive does not own.
type Applied struct {
	Primitive *Primitive
	Invariant Invariant
}

// AppliedTo lists the invariants whose applies covers file, leaving out
// those of primitives on file's own chain: those are shown already.
func (c *Registry) AppliedTo(file string) []Applied {
	chain := c.Chain(file)
	var out []Applied
	for _, p := range c.Primitives {
		if slices.Contains(chain, p) {
			continue
		}
		for _, inv := range p.Invariants {
			if matchAny(inv.Applies, file) {
				out = append(out, Applied{Primitive: p, Invariant: inv})
			}
		}
	}
	return out
}

// TwinsOf lists every twins entry, in any primitive, with a site in file.
func (c *Registry) TwinsOf(file string) []Twin {
	var out []Twin
	for _, p := range c.Primitives {
		for _, twin := range p.Twins {
			for _, site := range twin.Sites {
				if site.Path == file {
					out = append(out, twin)
					break
				}
			}
		}
	}
	return out
}

// Check is one judged question and the changed files it applies to.
type Check struct {
	Primitive string   `json:"primitive"`
	Invariant string   `json:"invariant"`
	Rule      string   `json:"rule"`
	Question  string   `json:"question"`
	Files     []string `json:"files"`
}

// Checks lists the judge questions that apply to the changed files:
// one per invariant on each file's owner chain or whose applies covers
// the file, with its files grouped.
func (c *Registry) Checks(changed []string) []Check {
	index := map[string]int{}
	var out []Check
	add := func(p *Primitive, inv Invariant, file string) {
		if inv.Judge == "" {
			return
		}
		key := p.ID + "/" + inv.ID
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, Check{Primitive: p.ID, Invariant: inv.ID, Rule: inv.Rule, Question: inv.Judge})
		}
		out[i].Files = append(out[i].Files, file)
	}
	for _, file := range changed {
		for _, p := range c.Chain(file) {
			for _, inv := range p.Invariants {
				add(p, inv, file)
			}
		}
		for _, a := range c.AppliedTo(file) {
			add(a.Primitive, a.Invariant, file)
		}
	}
	return out
}
