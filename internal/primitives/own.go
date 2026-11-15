package primitives

import (
	"path"
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

func (p *Primitive) Owns(file string) bool {
	for _, ref := range p.Code {
		if strings.HasSuffix(ref.Value, "/") {
			if strings.HasPrefix(file, ref.Value) {
				return true
			}
			continue
		}
		if ok, _ := path.Match(ref.Value, file); ok {
			return true
		}
	}
	return false
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
// one per invariant on each file's owner chain, with its files grouped.
func (c *Registry) Checks(changed []string) []Check {
	index := map[string]int{}
	var out []Check
	for _, file := range changed {
		for _, p := range c.Chain(file) {
			for _, inv := range p.Invariants {
				if inv.Judge == "" {
					continue
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
		}
	}
	return out
}
