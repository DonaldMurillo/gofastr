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

func TestThisCallableFieldsAndStaticContinuations(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   int
	}{
		{"arrow-expression", `class Panel { render = text => el.innerHTML = text; load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"arrow-block", `class Panel { render = (text) => { el.innerHTML = text; }; load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"function-field", `class Panel { render = function(text) { el.innerHTML = text; }; load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"named-function-field", `class Panel { render = function mountText(text) { el.innerHTML = text; }; load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"static-method", `class Panel { static render(text) { el.innerHTML = text; } static load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"static-field", `class Panel { static render = text => el.innerHTML = text; static load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"static-and-instance-targets", `class Panel { static render(text) { el.innerHTML = text; } render(text) { console.log(text); } static load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"instance-and-static-targets", `class Panel { render(text) { el.innerHTML = text; } static render(text) { console.log(text); } load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 1},
		{"static-and-instance-isolation", `class Panel { static render(text) { console.log(text); } render(text) { el.innerHTML = text; } static load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
		{"instance-and-static-isolation", `class Panel { render(text) { console.log(text); } static render(text) { el.innerHTML = text; } load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
		{"field-at-class-end", `class Panel { load(src) { fetch(src).then(r => r.text()).then(this.render); } render = text => el.innerHTML = text }`, 1},
		{"static-caller-field", `class Panel { static render(text) { el.innerHTML = text; } static load = src => { fetch(src).then(r => r.text()).then(this.render); }; }`, 1},
		{"instance-caller-field", `class Panel { render = text => el.innerHTML = text; load = src => { fetch(src).then(r => r.text()).then(this.render); }; }`, 1},
		{"static-target-on-instance", `class Panel { static render(text) { el.innerHTML = text; } load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
		{"instance-target-on-static", `class Panel { render(text) { el.innerHTML = text; } static load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
		{"static-field-on-instance", `class Panel { static render = text => el.innerHTML = text; load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
		{"instance-field-on-static", `class Panel { render = text => el.innerHTML = text; static load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
		{"guarded-field", `class Panel { render = text => el.innerHTML = text; load(src) { fetch(src).then(r => { if (!r.ok) throw Error(); return r.text(); }).then(this.render); } }`, 0},
		{"guarded-static", `class Panel { static render(text) { el.innerHTML = text; } static load(src) { fetch(src).then(r => { if (!r.ok) throw Error(); return r.text(); }).then(this.render); } }`, 0},
		{"cross-class-field", `class Other { render = text => el.innerHTML = text; } class Logger { render = text => console.log(text); load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
		{"cross-class-static", `class Other { static render(text) { el.innerHTML = text; } } class Logger { static render(text) { console.log(text); } static load(src) { fetch(src).then(r => r.text()).then(this.render); } }`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeRuntimeFixture(t, "continuations.js", test.source)
			result, err := LintResponseMountedAfterOK(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Violations) != test.want {
				t.Fatalf("got %d violations, want %d:\n%s", len(result.Violations), test.want, result.Error())
			}
		})
	}
}
