package ownstyle

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// SheetModel is the class-and-variant vocabulary of one owned sheet,
// extracted from its parsed rules. It is DATA ONLY: the generator
// (gofastr gen styles, slice 2) formats Go from it, nothing here emits
// code. The shape is stable and documented field by field so generated
// files stay reproducible across generator versions.
//
// The rules, from the design doc:
//
//   - Every class becomes a method returning its local name: .key is
//     Key(). Names are not mangled; the compiled @scope isolates them.
//   - In a compound selector the FIRST class is the base and the rest
//     are its variants (".column.over-limit": base column, flag
//     over-limit). ":scope" is the root pseudo-base: ":scope.fresh" is
//     a root flag.
//   - A variant class containing "--" is a group value: ".priority--
//     urgent" is group priority, value urgent. Each group becomes a
//     string type with constants and a Parse<Group> helper.
//   - A class may not be both a base and a variant in one file
//     (RuleClassTwice). One group name must carry the same value set
//     under every base that uses it (RuleGroupMismatch).
//
// Classes are collected from every compound of every style rule
// (descendant compounds included: ".column.over-limit .count"
// contributes the over-limit flag AND the count base), recursively
// through @media / @container / @supports blocks. Kit classes (fui-*)
// and element parts contribute nothing.
type SheetModel struct {
	// Root is the :scope vocabulary: its flags and the groups it uses.
	// Nil when no rule puts a variant on :scope.
	Root *RootModel

	// Classes lists every plain and base class in first-appearance
	// order. A class with no variants has empty Flags and Groups; a
	// base with variants carries them.
	Classes []*ClassModel

	// Groups lists the distinct group names in first-appearance order,
	// each with its values in first-appearance order.
	Groups []*GroupModel
}

// RootModel is the :scope pseudo-base and its variants.
type RootModel struct {
	// Doc is the doc comment of the rule that introduced :scope.
	Doc string
	// Flags are the root's flag classes (":scope.fresh").
	Flags []FlagModel
	// Groups names into SheetModel.Groups that :scope uses
	// (":scope.priority--urgent" adds "priority").
	Groups []string
}

// ClassModel is one class: a plain class (".key") or a base with
// variants (".column" of ".column.over-limit").
type ClassModel struct {
	// Name is the local class name without the dot.
	Name string
	// Doc is the doc comment of the rule that introduced the class, or
	// of the rule that introduced it as a base, whichever came first.
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

// GroupModel is one variant group (".priority--urgent" → group
// "priority"), shared by every base that names it with the same value
// set.
type GroupModel struct {
	// Name is the group name (the part before "--").
	Name string
	// Doc is the doc comment of the rule that introduced the group's
	// first value.
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

// Model extracts the variant model from a parsed sheet. Generator
// errors come back as Diagnostics (RuleClassTwice: a class both base
// and variant; RuleGroupMismatch: one group name with different value
// sets) with the position of the offending rule; the returned model is
// still complete enough to inspect. Parse-level problems are not
// re-reported: run Parse first.
func Model(sheet *Stylesheet) (*SheetModel, []Diagnostic) {
	m := &SheetModel{}
	b := &modelBuilder{m: m, roles: map[string]classRole{}}
	for _, r := range sheet.Rules {
		b.rule(r)
	}
	// A :scope with no variants leaves no root vocabulary.
	if m.Root != nil && len(m.Root.Flags) == 0 && len(m.Root.Groups) == 0 {
		m.Root = nil
	}
	b.finish()
	return m, b.diags
}

// classRole tracks how one class name has been used so far.
type classRole struct {
	asBase    bool
	asVariant bool
}

// modelBuilder walks the rules, folding classes into the model.
type modelBuilder struct {
	m     *SheetModel
	diags []Diagnostic

	// roles is every class name seen, with its base/variant usage.
	roles map[string]classRole
	// groupValuesByBase is group name → base name → its value set, for
	// the same-values rule across bases.
	groupValuesByBase map[string]map[string][]string
	// groupPos records where each group's declaration lives, for
	// mismatch diagnostics.
	groupPos map[string]Pos

	// lastBase is the base of the compound being folded ("" when the
	// compound's base is :scope).
	lastBase string
}

// rule folds one rule (recursing into at-rule blocks and nested style
// rules).
func (b *modelBuilder) rule(r *Rule) {
	if !r.At {
		b.selector(r)
	}
	for _, n := range r.Nested {
		b.rule(n)
	}
}

// selector folds one style rule's compounds. The rule's doc comment
// describes its first compound only: in ".column.over-limit .count" it
// documents the over-limit flag, not .count.
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

// compound folds one compound selector: the classes it carries, in
// order, the first being the base (or the :scope pseudo-base).
func (b *modelBuilder) compound(comp []Token, doc string) {
	sig := significantTokens(comp)
	isRoot := len(sig) >= 2 && sig[0].Type == TokenColon && sig[1].Type == TokenIdent && sig[1].Text == "scope"
	rest := sig
	if isRoot {
		rest = sig[2:]
	}
	var classIdx []int // indexes into sig of (dot, ident) pairs
	// A :has() argument is a relative selector naming other elements:
	// its classes are folded as selectors of their own once this
	// compound is done, never as this compound's variants. :is(),
	// :where() and :not() still describe this element, so their
	// classes stay in the scan.
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
			if strings.HasPrefix(rest[i+1].Text, "fui-") {
				continue // kit class: not this sheet's vocabulary
			}
			classIdx = append(classIdx, i)
		}
	}
	defer b.hasArguments(hasArgs)
	if isRoot {
		// The root model exists from the first :scope rule on, carrying
		// that rule's doc comment, even before any variant appears.
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

// hasArguments folds each :has() argument as a selector list of its
// own: ".grid:has(.cell.wide)" contributes the cell base with its wide
// flag. A leading combinator (":has(> .x)") splits off like any other.
func (b *modelBuilder) hasArguments(args [][]Token) {
	for _, arg := range args {
		for _, sel := range splitSelectors(arg) {
			for _, comp := range splitCompounds(sel) {
				b.compound(comp, "")
			}
		}
	}
}

// noteBase records a class used as a base (or introduces it).
func (b *modelBuilder) noteBase(name, doc string, dot Token) {
	role := b.roles[name]
	if role.asVariant {
		b.reportClassTwice(name, dot)
	}
	role.asBase = true
	b.roles[name] = role
	if c := b.class(name); c != nil {
		// A class first seen undocumented (a descendant compound) takes
		// the doc of the first rule that documents it.
		if c.Doc == "" {
			c.Doc = doc
		}
		return
	}
	b.m.Classes = append(b.m.Classes, &ClassModel{Name: name, Doc: doc})
}

// noteVariant records a class used as a variant of the preceding base
// in its compound. rootBase marks a variant on :scope.
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
	// A flag lands on the last base introduced in this compound; the
	// compound is processed left to right, so the most recent class in
	// Classes from this compound is the base. Record it via the base
	// that noteBase just appended (the LAST class in Classes whose
	// variant this is). Track it through the builder's lastBase.
	if b.lastBase != "" {
		if c := b.class(b.lastBase); c != nil && !hasFlag(c.Flags, name) {
			c.Flags = append(c.Flags, flag)
		}
	}
}

// reportClassTwice emits the both-base-and-variant diagnostic.
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

// class finds a class model by name.
func (b *modelBuilder) class(name string) *ClassModel {
	for _, c := range b.m.Classes {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ensureRoot returns the root model, creating it on first use. The doc
// argument is used only at creation.
func (b *modelBuilder) ensureRoot(doc string) *RootModel {
	if b.m.Root == nil {
		b.m.Root = &RootModel{Doc: doc}
	}
	return b.m.Root
}

// addGroupValue folds one "group--value" variant into the model.
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
	// The same-value-set rule is checked in finish, once every base's
	// set is known; remember which base declared what.
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
		// :scope is its own base.
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
	// A base using a group records the group on the class model.
	if b.lastBase != "" {
		if c := b.class(b.lastBase); c != nil {
			c.Groups = appendUnique(c.Groups, group)
		}
	}
}

// group finds a group model by name.
func (b *modelBuilder) group(name string) *GroupModel {
	for _, g := range b.m.Groups {
		if g.Name == name {
			return g
		}
	}
	return nil
}

// finish validates the group value sets across bases.
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

// sameValueSet compares two value sets ignoring order.
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

// splitGroupValue splits a group-value class name ("priority--urgent")
// into ("priority", "urgent"). ok is false for plain flags.
func splitGroupValue(name string) (group, value string, ok bool) {
	i := strings.Index(name, "--")
	if i <= 0 || i+2 >= len(name) {
		return "", "", false
	}
	return name[:i], name[i+2:], true
}

// appendUnique appends s when absent, preserving order.
func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// hasFlag reports whether a flag list already carries the name.
func hasFlag(flags []FlagModel, name string) bool {
	for _, f := range flags {
		if f.Name == name {
			return true
		}
	}
	return false
}

// class finds a class in the model by name (test and generator
// convenience; linear scan, models are small).
func (m *SheetModel) class(name string) *ClassModel {
	for _, c := range m.Classes {
		if c.Name == name {
			return c
		}
	}
	return nil
}
