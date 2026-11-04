package check

import "testing"

func TestThisMethodContinuationsMountOnlyTheirClass(t *testing.T) {
	directory := writeRuntimeFixture(t, "continuations.js", `
class Logger {
  render(text) { console.log(text); }
  load(src) { fetch(src).then(r => r.text()).then(this.render); }
}
class Panel {
  render(text) { el.innerHTML = text; }
  load(src) { fetch(src).then(r => r.text()).then(this.render); }
}
class OtherLogger {
  render(text) { console.log(text); }
  load(src) { fetch(src).then(r => r.text()).then(this.render); }
}
class Guarded {
  render(text) { el.innerHTML = text; }
  load(src) { fetch(src).then(r => { if (!r.ok) throw Error(); return r.text(); }).then(this.render); }
}
`)
	result, err := LintResponseMountedAfterOK(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected only Panel to mount without a gate, got %d:\n%s", len(result.Violations), result.Error())
	}
	if result.Violations[0].Line != 8 {
		t.Fatalf("finding borrowed another class's method: %+v", result.Violations[0])
	}
}
