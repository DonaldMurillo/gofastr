package check

import (
	"fmt"
	"regexp"
)

// LintLegacyIdioms fires on the pre-ES2015 spellings the shipped
// JavaScript has a modern form for. The runtime targets ES2020, and
// the no-var lint already holds `var` out; these are the idioms that
// travelled with it:
//
//   - `.push.apply(x, list)`: spread the list, x.push(...list);
//   - `Array.prototype.<m>.call/apply(list)`: spread the list into an
//     array, [...list], or call Array.from;
//   - the `arguments` object: take rest parameters, function (...a),
//     and pass them on with .apply(this, a);
//   - an assignment to `<Ctor>.prototype.<name>`: declare the method in
//     a class body.
//
// Silent on:
//   - Object.prototype.hasOwnProperty.call(o, k): the own-key check the
//     security lints require (Object.hasOwn is ES2022, past the target);
//   - a built-in's method borrowed onto a value that may shadow it,
//     such as HTMLFormElement.prototype.submit.call(form) past a field
//     named "submit": only Array.prototype borrows have a spread form;
//   - `arguments` as a property name (msg.arguments, { arguments: … }),
//     which MCP's tools/call envelope carries;
//   - comments and string literals.
func LintLegacyIdioms(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		for _, r := range legacyIdiomRules {
			for _, loc := range r.re.FindAllStringSubmatchIndex(f.Blank, -1) {
				// The arguments rule captures the identifier itself;
				// the rest report the whole match.
				at := loc[0]
				if len(loc) > 2 && loc[2] >= 0 {
					at = loc[2]
				}
				res.add(f.Path, f.lineOf(at), fmt.Sprintf("[legacy-idiom] %s", r.msg))
			}
		}
	}
	return res, nil
}

var legacyIdiomRules = []struct {
	re  *regexp.Regexp
	msg string
}{
	{
		regexp.MustCompile(`\.\s*push\s*\.\s*apply\s*\(`),
		"`.push.apply(x, list)` is the pre-spread spelling; write x.push(...list)",
	},
	{
		regexp.MustCompile(`\bArray\s*\.\s*prototype\s*\.\s*[A-Za-z_$][\w$]*\s*\.\s*(?:call|apply)\s*\(`),
		"borrowing an Array.prototype method onto a list is the pre-spread spelling; spread it, [...list], or call Array.from(list)",
	},
	{
		// Not after a '.', and not a property key: `arguments:` in an
		// object literal is a name, not the arguments object.
		regexp.MustCompile(`(?:^|[^\w$.])(arguments)\b(?:\s*[^\s:]|\s*$)`),
		"the `arguments` object is the pre-rest spelling; take rest parameters, function (...a), and pass them on with .apply(this, a)",
	},
	{
		// An assignment, not a comparison: one '=' not followed by
		// another '=' or a '>'.
		regexp.MustCompile(`\.\s*prototype\s*\.\s*[A-Za-z_$][\w$]*\s*=[^=>]`),
		"assigning to `<Ctor>.prototype.<name>` is the pre-class spelling; declare the method in a class body",
	},
}
