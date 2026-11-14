// Package canon reads the contributor canon under canon/: one
// primitive.yml per primitive, nested by directory, each carrying the
// primitive's invariants and decisions. canon/README.md is the format.
package canon

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

const (
	Dir           = "canon"
	PrimitiveFile = "primitive.yml"
)

type Canon struct {
	Root       string
	Primitives []*Primitive
	byID       map[string]*Primitive
	fsys       fs.FS
}

type Primitive struct {
	ID         string
	File       string
	Line       int
	Summary    string
	Code       []Ref
	Docs       []Ref
	Imports    []ImportRule
	Invariants []Invariant
	Decisions  []Decision
}

// Ref is a path or pattern with the line that declared it.
type Ref struct {
	Value string
	Line  int
}

// ImportRule bans one package from the dependency closure of others.
type ImportRule struct {
	Deny    string
	From    []Ref
	Why     string
	Instead string
	Line    int
}

type Invariant struct {
	ID         string
	Rule       string
	EnforcedBy []Enforcer
	Judge      string
	Line       int
}

// Enforcer names what fails when an invariant is broken.
type Enforcer struct {
	Kind string
	Ref  string
	Line int
}

const (
	KindTest     = "test"
	KindAnalyzer = "analyzer"
	KindContract = "contract"
)

type Decision struct {
	ID           string
	Date         string
	Status       string
	Decision     string
	Rejected     []Rejected
	RevisitIf    string
	Upholds      []Ref
	SupersededBy string
	Line         int
}

type Rejected struct {
	Option string
	Why    string
}

const (
	StatusAccepted   = "accepted"
	StatusSuperseded = "superseded"
)

// Finding is one problem in the canon, addressed as file:line.
type Finding struct {
	File string
	Line int
	Msg  string
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Msg) }

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Load reads every primitive under root/canon. Content problems come
// back as findings; only a tree that cannot be read is an error.
func Load(root string) (*Canon, []Finding, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, err
	}
	c := &Canon{Root: root, byID: map[string]*Primitive{}, fsys: dir.FS()}
	var findings []Finding
	err = fs.WalkDir(c.fsys, Dir, func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != PrimitiveFile {
			return nil
		}
		src, err := fs.ReadFile(c.fsys, rel)
		if err != nil {
			return err
		}
		dir := strings.TrimPrefix(path.Dir(rel), Dir)
		id := strings.ReplaceAll(strings.Trim(dir, "/"), "/", ".")
		if prim := parsePrimitive(rel, id, src, &findings); prim != nil {
			c.Primitives = append(c.Primitives, prim)
			c.byID[prim.ID] = prim
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(c.Primitives, func(a, b *Primitive) int { return strings.Compare(a.ID, b.ID) })
	return c, findings, nil
}

func (c *Canon) Primitive(id string) *Primitive { return c.byID[id] }

// Parent returns the primitive one level up, or nil at the top.
func (c *Canon) Parent(p *Primitive) *Primitive {
	i := strings.LastIndex(p.ID, ".")
	if i < 0 {
		return nil
	}
	return c.byID[p.ID[:i]]
}

// Children returns the primitives exactly one level below id; the
// empty id lists the top level.
func (c *Canon) Children(id string) []*Primitive {
	var out []*Primitive
	for _, p := range c.Primitives {
		if parentID(p.ID) == id {
			out = append(out, p)
		}
	}
	return out
}

func parentID(id string) string {
	i := strings.LastIndex(id, ".")
	if i < 0 {
		return ""
	}
	return id[:i]
}

func depth(id string) int { return strings.Count(id, ".") + 1 }

type decoder struct {
	file     string
	findings *[]Finding
}

func (d decoder) errf(line int, format string, args ...any) {
	*d.findings = append(*d.findings, Finding{File: d.file, Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (d decoder) mapping(n *coreyaml.Node, what string, allowed ...string) bool {
	if n.Kind != coreyaml.Map {
		d.errf(n.Line, "%s must be a mapping", what)
		return false
	}
	for _, k := range slices.Sorted(maps.Keys(n.Map)) {
		if !slices.Contains(allowed, k) {
			d.errf(n.Map[k].Line, "unknown key %q in %s (allowed: %s)", k, what, strings.Join(allowed, ", "))
		}
	}
	return true
}

func (d decoder) scalar(n *coreyaml.Node, what string) string {
	if n.Kind != coreyaml.Scalar {
		d.errf(n.Line, "%s must be a string", what)
		return ""
	}
	switch v := n.Value.(type) {
	case string:
		return strings.TrimSpace(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		d.errf(n.Line, "%s must be a quoted string (YAML read it as %T)", what, n.Value)
		return ""
	}
}

func (d decoder) str(n *coreyaml.Node, key string, required bool) string {
	v := n.Map[key]
	if v == nil {
		if required {
			d.errf(n.Line, "missing %s", key)
		}
		return ""
	}
	s := d.scalar(v, key)
	if s == "" && required {
		d.errf(v.Line, "%s is empty", key)
	}
	return s
}

func (d decoder) list(n *coreyaml.Node, key string) []*coreyaml.Node {
	v := n.Map[key]
	if v == nil {
		return nil
	}
	if v.Kind != coreyaml.List {
		d.errf(v.Line, "%s must be a list", key)
		return nil
	}
	return v.List
}

func (d decoder) refs(n *coreyaml.Node, key string) []Ref {
	var out []Ref
	for _, item := range d.list(n, key) {
		if s := d.scalar(item, key+" entry"); s != "" {
			out = append(out, Ref{Value: s, Line: item.Line})
		}
	}
	return out
}

func (d decoder) id(n *coreyaml.Node, what string) string {
	id := d.str(n, "id", true)
	if id != "" && !idPattern.MatchString(id) {
		d.errf(n.Map["id"].Line, "%s id %q must be kebab-case (a-z, 0-9, single hyphens)", what, id)
	}
	return id
}

func parsePrimitive(file, id string, src []byte, findings *[]Finding) *Primitive {
	d := decoder{file: file, findings: findings}
	root, err := coreyaml.Parse(string(src))
	if err != nil {
		d.errf(1, "%v", err)
		return nil
	}
	if !d.mapping(root, "primitive", "primitive", "summary", "code", "docs", "imports", "invariants", "decisions") {
		return nil
	}
	p := &Primitive{ID: id, File: file, Line: root.Line}
	if id == "" {
		d.errf(root.Line, "%s sits at the canon root: a primitive needs its own directory", PrimitiveFile)
	}
	for seg := range strings.SplitSeq(id, ".") {
		if id != "" && !idPattern.MatchString(seg) {
			d.errf(root.Line, "directory %q is not a kebab-case primitive name", seg)
		}
	}
	if declared := d.str(root, "primitive", true); declared != "" && declared != id {
		d.errf(root.Map["primitive"].Line, "primitive is %q but the directory makes it %q", declared, id)
	}
	p.Summary = d.str(root, "summary", true)
	p.Code = d.refs(root, "code")
	p.Docs = d.refs(root, "docs")
	for _, n := range d.list(root, "imports") {
		if !d.mapping(n, "imports entry", "deny", "from", "why", "instead") {
			continue
		}
		p.Imports = append(p.Imports, ImportRule{
			Deny:    d.str(n, "deny", true),
			From:    d.refs(n, "from"),
			Why:     d.str(n, "why", true),
			Instead: d.str(n, "instead", true),
			Line:    n.Line,
		})
	}
	for _, n := range d.list(root, "invariants") {
		if !d.mapping(n, "invariant", "id", "rule", "enforced_by", "judge") {
			continue
		}
		inv := Invariant{ID: d.id(n, "invariant"), Rule: d.str(n, "rule", true), Judge: d.str(n, "judge", false), Line: n.Line}
		for _, e := range d.list(n, "enforced_by") {
			if !d.mapping(e, "enforced_by entry", KindTest, KindAnalyzer, KindContract) {
				continue
			}
			if len(e.Map) != 1 {
				d.errf(e.Line, "enforced_by entry names exactly one of test, analyzer, contract")
				continue
			}
			for _, kind := range slices.Sorted(maps.Keys(e.Map)) {
				inv.EnforcedBy = append(inv.EnforcedBy, Enforcer{Kind: kind, Ref: d.scalar(e.Map[kind], kind), Line: e.Line})
			}
		}
		p.Invariants = append(p.Invariants, inv)
	}
	for _, n := range d.list(root, "decisions") {
		if !d.mapping(n, "decision", "id", "date", "status", "decision", "rejected", "revisit_if", "upholds", "superseded_by") {
			continue
		}
		dec := Decision{
			ID:           d.id(n, "decision"),
			Date:         d.str(n, "date", false),
			Status:       d.str(n, "status", true),
			Decision:     d.str(n, "decision", true),
			RevisitIf:    d.str(n, "revisit_if", false),
			Upholds:      d.refs(n, "upholds"),
			SupersededBy: d.str(n, "superseded_by", false),
			Line:         n.Line,
		}
		for _, r := range d.list(n, "rejected") {
			if !d.mapping(r, "rejected entry", "option", "why") {
				continue
			}
			dec.Rejected = append(dec.Rejected, Rejected{Option: d.str(r, "option", true), Why: d.str(r, "why", true)})
		}
		p.Decisions = append(p.Decisions, dec)
	}
	return p
}
