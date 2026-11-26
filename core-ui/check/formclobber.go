package check

import (
	"fmt"
	"regexp"
	"strings"
)

// LintFormClobber fires on a form method or behaviour property reached through the form itself:
// form.requestSubmit(), f.reset(), el.form.elements, closest('form').action. A control named
// after the member shadows it, so the call throws and the read returns the control. A plain
// write to a property stays quiet: the prototype's setter still runs.
func LintFormClobber(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		allowed := allowedLines(jsComments(f), "formclobber")
		for _, loc := range reFormMember.FindAllStringSubmatchIndex(f.Blank, -1) {
			if end := loc[1]; end < len(f.Blank) && isJSIdentChar(f.Blank[end]) {
				continue
			}
			member := f.Blank[loc[2]:loc[3]]
			if !formClobberMethods[member] && plainAssignment(f.Blank, loc[1]) {
				continue // the prototype's setter still runs: a write reaches the attribute
			}
			recv, ok := formReceiver(f, loc[0])
			if !ok {
				continue
			}
			line := f.lineOf(loc[0])
			if allowed[line] {
				continue
			}
			res.add(f.Path, line, fmt.Sprintf("[form-clobber] %s.%s goes through the form, where a control named %q replaces the member: %s; %s", recv, member, member, formClobberEffect(member), formClobberFix(member)))
		}
	}
	return res, nil
}

var reFormMember = regexp.MustCompile(`\??\.\s*(requestSubmit|submit|reset|checkValidity|reportValidity|action|method|target|enctype|elements)`)

var formClobberMethods = map[string]bool{
	"requestSubmit": true, "submit": true, "reset": true, "checkValidity": true, "reportValidity": true,
}

func formClobberEffect(member string) string {
	if formClobberMethods[member] {
		return "the call throws, and a feature check finds the control"
	}
	return "the read returns the control"
}

func formClobberFix(member string) string {
	switch {
	case formClobberMethods[member]:
		return fmt.Sprintf("call HTMLFormElement.prototype.%s.call(form, …)", member)
	case member == "elements":
		return "read Object.getOwnPropertyDescriptor(HTMLFormElement.prototype, 'elements').get.call(form)"
	default:
		return fmt.Sprintf("read form.getAttribute(%q), or the runtime's _formAttr, which also reads the submitter's form%s", member, member)
	}
}

// formName reports whether name reads as a form: form, a form… camel-case name (formSource,
// formEl) or a …Form one (searchForm).
func formName(name string) bool {
	if rest, ok := strings.CutPrefix(name, "form"); ok {
		return rest == "" || (rest[0] >= 'A' && rest[0] <= 'Z') || rest[0] == '_' || (rest[0] >= '0' && rest[0] <= '9')
	}
	return strings.HasSuffix(name, "Form") && name != "Form"
}

// plainAssignment reports whether the member ending at end is the target of a plain = (not ==,
// ===, =>, or a compound assignment, which reads first).
func plainAssignment(blank string, end int) bool {
	k := skipSpace(blank, end)
	return k < len(blank) && blank[k] == '=' && (k+1 >= len(blank) || (blank[k+1] != '=' && blank[k+1] != '>'))
}

var reFormSelectorCall = regexp.MustCompile(`\b(?:closest|querySelector)\s*$`)

var reFormSelector = regexp.MustCompile(`^\(\s*(['"` + "`" + `])form(?:[\[.#:\s][^'"` + "`" + `]*)?['"` + "`" + `]\s*\)$`)

// formReceiver reports whether the expression before the dot at dot is plausibly a form, and how
// to name it in the finding: a variable named f, frm or as formName reads it, a member formName reads
// (el.form, e.target.form), closest('form') or querySelector('form…'), or document.forms[…].
func formReceiver(f jsSource, dot int) (string, bool) {
	j := skipSpaceBack(f.Blank, dot-1)
	if j < 0 {
		return "", false
	}
	switch c := f.Blank[j]; {
	case isJSIdentChar(c):
		end := j + 1
		for j >= 0 && isJSIdentChar(f.Blank[j]) {
			j--
		}
		name := f.Blank[j+1 : end]
		k := skipSpaceBack(f.Blank, j)
		member := k >= 0 && f.Blank[k] == '.' && (k == 0 || f.Blank[k-1] != '.')
		if formName(name) {
			return name, true
		}
		if !member && (name == "f" || name == "frm") {
			return name, true
		}
		return "", false
	case c == ')':
		open := matchDelimBack(f.Blank, j)
		if open < 0 || !reFormSelectorCall.MatchString(f.Blank[max(0, open-40):open]) {
			return "", false
		}
		if !reFormSelector.MatchString(f.Code[open : j+1]) {
			return "", false
		}
		return strings.Join(strings.Fields(f.Code[open-len(reFormSelectorCall.FindString(f.Blank[max(0, open-40):open])):j+1]), ""), true
	case c == ']':
		open := matchDelimBack(f.Blank, j)
		if open < 0 {
			return "", false
		}
		k := skipSpaceBack(f.Blank, open-1)
		if k < 4 || f.Blank[k-4:k+1] != "forms" || (k >= 5 && isJSIdentChar(f.Blank[k-5])) {
			return "", false
		}
		return "forms[…]", true
	}
	return "", false
}
