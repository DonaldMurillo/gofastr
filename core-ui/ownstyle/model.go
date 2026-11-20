package ownstyle

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// SheetModel is the class-and-variant vocabulary of one owned sheet, extracted
// from its parsed rules.
type SheetModel struct {
	// Root is the :scope vocabulary.
	Root *RootModel

	// Classes lists every plain and base class in first-appearance order.
	Classes []*ClassModel

	// Groups lists the distinct group names in first-appearance order, each with
	// its values in first-appearance order.
	Groups []*GroupModel
}

// RootModel is the :scope pseudo-base and its variants.
type RootModel struct {
	// Doc is the doc comment of the rule that introduced :scope.
	Doc string
	// Flags are the root's flag classes (":scope.fresh").
	Flags []FlagModel
	// Groups names into SheetModel.Groups that :scope uses (":scope.priority--
	// urgent" adds "priority").
	Groups []string
}

// ClassModel is one class.
type ClassModel struct {
	// Name is the local class name without the dot.
	Name string
	// Doc is the doc comment of the rule that introduced the class, or of the
	// rule that introduced it as a base, whichever came first.
	Doc string
	// Flags are the base's flag variants in first-appearance order.
	Flags []FlagModel
	// Groups names into SheetModel.Groups that this base uses.
	Groups []string
}

// FlagModel is a boolean variant class (".fresh", ".over-limit").
type FlagModel struct {
	// Name is the local class name without the dot.
	Name string
	// Doc is the doc comment of the rule that introduced the flag.
	Doc string
}

// GroupModel is one variant group (".priority--urgent" → group "priority"),
// shared by every base that names it with the same value set.
type GroupModel struct {
	// Name is the group name (the part before "--").
	Name string
	// Doc is the doc comment of the rule that introduced the group's first value.
	Doc string
	// Values are the declared values in first-appearance order.
	Values []GroupValueModel
}

// GroupValueModel is one value of a group.
type GroupValueModel struct {
	// Value is the local part after "--" ("urgent").
	Value string
	// Doc is the doc comment of the rule that introduced the value.
	Doc string
}

// Model extracts the variant model from a parsed sheet.
func Model(sheet *Stylesheet) (*SheetModel, []Diagnostic) {
	m := &SheetModel{}
	b := &modelBuilder{m: m, roles: map[string]classRole{}}
	for _, r := range sheet.Rules {
		b.rule(r)
	}
	if m.Root != nil && len(m.Root.Flags) == 0 && len(m.Root.Groups) == 0 {
		m.Root = nil
	}
	b.finish()
	return m, b.diags
}

type classRole struct {
	asBase    bool
	asVariant bool
}

type modelBuilder struct {
	m     *SheetModel
	diags []Diagnostic

	roles             map[string]classRole
	groupValuesByBase map[string]map[string][]string
	groupPos          map[string]Pos

	lastBase string
}

func (b *modelBuilder) rule(r *Rule) {
	if !r.At {
		b.selector(r)
	}
	for _, n := range r.Nested {
		b.rule(n)
	}
}

func (b *modelBuilder) selector(r *Rule) {
	for _, sel := range splitSelectors(r.Prel) {
		for i, comp := range splitCompounds(sel) {
			doc := r.Doc
			if i > 0 {
				doc = ""
			}
			b.compound(comp, doc)
		}
	}
}

func (b *modelBuilder) compound(comp []Token, doc string) {
	sig := significantTokens(comp)
	isRoot := len(sig) >= 2 && sig[0].Type == TokenColon && sig[1].Type == TokenIdent && sig[1].Text == "scope"
	rest := sig
	if isRoot {
		rest = sig[2:]
	}
	var classIdx []int
	var hasArgs [][]Token
	for i := 0; i+1 < len(rest); i++ {
		if rest[i].Type == TokenFunction && strings.EqualFold(rest[i].Text, "has(") {
			depth, j := 1, i+1
			for ; j < len(rest) && depth > 0; j++ {
				switch rest[j].Type {
				case TokenFunction, TokenOpenParen, TokenOpenSquare:
					depth++
				case TokenCloseParen, TokenCloseSquare:
					depth--
				}
			}
			hasArgs = append(hasArgs, rest[i+1:j-1])
			i = j - 1
			continue
		}
		if rest[i].Type == TokenDelim && rest[i].Text == "." && rest[i+1].Type == TokenIdent {
			if isKitClass(rest[i+1].Text) {
				continue
			}
			classIdx = append(classIdx, i)
		}
	}
	defer b.hasArguments(hasArgs)
	if isRoot {
		b.ensureRoot(doc)
	}
	if len(classIdx) == 0 {
		return
	}
	if !isRoot {
		b.lastBase = rest[classIdx[0]+1].Text
	}
	first := true
	for _, i := range classIdx {
		dot, name := rest[i], rest[i+1]
		if first && !isRoot {
			b.noteBase(name.Text, doc, dot)
		} else {
			b.noteVariant(name.Text, doc, dot, isRoot && first)
		}
		first = false
	}
	b.lastBase = ""
}

func (b *modelBuilder) hasArguments(args [][]Token) {
	for _, arg := range args {
		for _, sel := range splitSelectors(arg) {
			for _, comp := range splitCompounds(sel) {
				b.compound(comp, "")
			}
		}
	}
}

func (b *modelBuilder) noteBase(name, doc string, dot Token) {
	role := b.roles[name]
	if role.asVariant {
		b.reportClassTwice(name, dot)
	}
	role.asBase = true
	b.roles[name] = role
	if c := b.class(name); c != nil {
		if c.Doc == "" {
			c.Doc = doc
		}
		return
	}
	b.m.Classes = append(b.m.Classes, &ClassModel{Name: name, Doc: doc})
}

func (b *modelBuilder) noteVariant(name, doc string, dot Token, rootBase bool) {
	role := b.roles[name]
	if role.asBase {
		b.reportClassTwice(name, dot)
	}
	role.asVariant = true
	b.roles[name] = role

	if group, value, ok := splitGroupValue(name); ok {
		if rootBase {
			b.ensureRoot(doc)
			b.m.Root.Groups = appendUnique(b.m.Root.Groups, group)
		}
		b.addGroupValue(group, value, doc, dot)
		return
	}
	flag := FlagModel{Name: name, Doc: doc}
	if rootBase {
		root := b.ensureRoot(doc)
		if !hasFlag(root.Flags, name) {
			root.Flags = append(root.Flags, flag)
		}
		return
	}
	if b.lastBase != "" {
		if c := b.class(b.lastBase); c != nil && !hasFlag(c.Flags, name) {
			c.Flags = append(c.Flags, flag)
		}
	}
}

func (b *modelBuilder) reportClassTwice(name string, dot Token) {
	for _, d := range b.diags {
		if d.Rule == RuleClassTwice && strings.Contains(d.Message, "."+name+" ") {
			return
		}
	}
	b.diags = append(b.diags, Diagnostic{
		Rule:     RuleClassTwice,
		Severity: SeverityError,
		Line:     dot.Line,
		Col:      dot.Col,
		Message: fmt.Sprintf(".%s is used as both a base and a variant in one file; a class is one or the other",
			name),
	})
}

func (b *modelBuilder) class(name string) *ClassModel {
	for _, c := range b.m.Classes {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (b *modelBuilder) ensureRoot(doc string) *RootModel {
	if b.m.Root == nil {
		b.m.Root = &RootModel{Doc: doc}
	}
	return b.m.Root
}

func (b *modelBuilder) addGroupValue(group, value, doc string, dot Token) {
	g := b.group(group)
	if g == nil {
		g = &GroupModel{Name: group, Doc: doc}
		b.m.Groups = append(b.m.Groups, g)
		if b.groupValuesByBase == nil {
			b.groupValuesByBase = map[string]map[string][]string{}
			b.groupPos = map[string]Pos{}
		}
		b.groupPos[group] = Pos{Line: dot.Line, Col: dot.Col}
	}
	for _, v := range g.Values {
		if v.Value == value {
			return
		}
	}
	g.Values = append(g.Values, GroupValueModel{Value: value, Doc: doc})
	if b.lastBase != "" {
		if b.groupValuesByBase == nil {
			b.groupValuesByBase = map[string]map[string][]string{}
		}
		byBase := b.groupValuesByBase[group]
		if byBase == nil {
			byBase = map[string][]string{}
			b.groupValuesByBase[group] = byBase
		}
		if !slices.Contains(byBase[b.lastBase], value) {
			byBase[b.lastBase] = append(byBase[b.lastBase], value)
		}
	} else {
		if b.groupValuesByBase == nil {
			b.groupValuesByBase = map[string]map[string][]string{}
		}
		byBase := b.groupValuesByBase[group]
		if byBase == nil {
			byBase = map[string][]string{}
			b.groupValuesByBase[group] = byBase
		}
		if !slices.Contains(byBase[":scope"], value) {
			byBase[":scope"] = append(byBase[":scope"], value)
		}
	}
	if b.lastBase != "" {
		if c := b.class(b.lastBase); c != nil {
			c.Groups = appendUnique(c.Groups, group)
		}
	}
}

func (b *modelBuilder) group(name string) *GroupModel {
	for _, g := range b.m.Groups {
		if g.Name == name {
			return g
		}
	}
	return nil
}

func (b *modelBuilder) finish() {
	for group, byBase := range b.groupValuesByBase {
		if len(byBase) < 2 {
			continue
		}
		var reference []string
		refBase := ""
		for _, base := range slices.Sorted(maps.Keys(byBase)) {
			if refBase == "" {
				refBase, reference = base, byBase[base]
			}
		}
		for base, values := range byBase {
			if base == refBase {
				continue
			}
			if !sameValueSet(reference, values) {
				pos := b.groupPos[group]
				b.diags = append(b.diags, Diagnostic{
					Rule:     RuleGroupMismatch,
					Severity: SeverityError,
					Line:     pos.Line,
					Col:      pos.Col,
					Message: fmt.Sprintf("group --%s declares different values under .%s and .%s; one group name means one value set",
						group, refBase, base),
				})
			}
		}
	}
	slices.SortFunc(b.diags, func(a, b Diagnostic) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Col - b.Col
	})
}

func sameValueSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := slices.Clone(a)
	sb := slices.Clone(b)
	slices.Sort(sa)
	slices.Sort(sb)
	return slices.Equal(sa, sb)
}

func splitGroupValue(name string) (group, value string, ok bool) {
	i := strings.Index(name, "--")
	if i <= 0 || i+2 >= len(name) {
		return "", "", false
	}
	return name[:i], name[i+2:], true
}

func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

func hasFlag(flags []FlagModel, name string) bool {
	for _, f := range flags {
		if f.Name == name {
			return true
		}
	}
	return false
}

func (m *SheetModel) class(name string) *ClassModel {
	for _, c := range m.Classes {
		if c.Name == name {
			return c
		}
	}
	return nil
}
