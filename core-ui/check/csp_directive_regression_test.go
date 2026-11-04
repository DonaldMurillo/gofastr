package check

import "testing"

func TestCSPDirectiveRequiresRealComment(t *testing.T) {
	for _, source := range []string{
		"package app\nvar doc = `\n//check-csp:ignore-file\n`\nvar markup = `<div style=\"display:none\"></div><script>alert(1)</script>`\n",
		"package app\n/*\n//check-csp:ignore-file\n*/\nvar markup = `<div style=\"display:none\"></div><script>alert(1)</script>`\n",
	} {
		directory := t.TempDir()
		writeFixture(t, directory, "page.go", source)
		styles, err := LintNoInlineStyles(directory)
		if err != nil || !styles.HasErrors() {
			t.Errorf("fake directive suppressed styles: result=%+v err=%v", styles, err)
		}
		scripts, err := LintNoInlineScripts(directory)
		if err != nil || !scripts.HasErrors() {
			t.Errorf("fake directive suppressed scripts: result=%+v err=%v", scripts, err)
		}
	}
}
