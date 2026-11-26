package check

import "testing"

// formClobberFixture: the 0b88cf01 pre-fix search clear (feature check
// and call through the form), the other S1b-02 sites' shapes under new
// names, the props a runtime reads for behaviour, and the quiet
// spellings: the prototype call, the attribute read, a plain write
// (the prototype's setter still runs), a non-form receiver, object keys
// and a marked site.
const formClobberFixture = `(() => {
  function clear(form) {
    if (shown) { if (form.requestSubmit) form.requestSubmit(); else form.submit(); }
  }
  function wire(el, e) {
    el.form.reset();
    e.target.form?.checkValidity();
    const ok = searchForm.reportValidity();
    for (const c of [...frm.elements]) use(c);
  }
  function route(btn) {
    const to = btn.closest('form').action;
    if (document.querySelector("form[data-x]").method === 'post') go();
    open(document.forms[0].target);
    return f.enctype;
  }
  function fixed(form, submitter) {
    HTMLFormElement.prototype.requestSubmit.call(form, submitter || undefined);
    const proto = HTMLFormElement.prototype;
    (proto.requestSubmit || proto.submit).call(form);
    const els = Object.getOwnPropertyDescriptor(HTMLFormElement.prototype, 'elements').get.call(form);
    const action = form.getAttribute('action');
    form.setAttribute('method', 'post');
    form.action = '/saved';
  }
  function quiet(e, btn, opts, fields) {
    e.target.reset();
    btn.closest('.card').submit;
    opts.form = { submit: true, action: '/x' };
    fields.f.method;
    form.submitter;
    form.targets;
    e.submitter.formAction;
    Form.submit();
  }
  function marked(form) {
    //gofastr:allow(formclobber) the login form renders every control and none is named submit
    form.submit();
  }
})();
`

func TestLintFormClobber_FiresThroughTheForm(t *testing.T) {
	dir := writeRuntimeFixture(t, "forms.js", formClobberFixture)
	res, err := LintFormClobber(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The pre-fix clear: feature check, call and fallback (3, three
	// findings); el.form (6), e.target.form?. (7), a …Form name (8), a
	// spread frm (9); closest('form') (12), querySelector('form[…]') (13),
	// document.forms[…] (14), f (15).
	assertLines(t, res, []int{3, 3, 3, 6, 7, 8, 9, 12, 13, 14, 15})
}

// TestLintFormClobber_Repo holds the shipped JavaScript to the rule: a
// form's own members are reached through HTMLFormElement.prototype or
// its attributes, never through the form a control can shadow.
//
//gofastr:test-reads core-ui/runtime/
//gofastr:test-reads *.js
func TestLintFormClobber_Repo(t *testing.T) {
	if testing.Short() {
		t.Skip("walks the repo")
	}
	lintRuntimeRepo(t, "a form member is reached through the form, where a named control shadows it", LintFormClobber)
}
