package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The wizard's form is novalidate because the server owns validation,
// so the handler has to actually do it: a blank or malformed value on
// the submitted step re-renders that step with the field's error set
// and the summary populated, and the flow neither advances nor
// confirms. Drives the real router, the same entry the deployed
// server uses.
func TestWizardDemoValidatesBeforeAdvancing(t *testing.T) {
	wizardDemoReset()
	app := newTestApp(t)

	post := func(body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, wizardDemoPath, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// An empty step-one submit stays on step one with both messages.
	code, body := post("wizard_action=next&_step=0")
	if code != http.StatusOK {
		t.Fatalf("empty submit = %d, want 200", code)
	}
	if !strings.Contains(body, "Personal info") {
		t.Errorf("an empty step-one submit advanced past step one:\n%s", firstN(body, 600))
	}
	if !strings.Contains(body, "Your full name is required.") || !strings.Contains(body, "Your email is required.") {
		t.Errorf("the blank fields' messages are missing:\n%s", firstN(body, 600))
	}
	// The summary and its focus hook are on the page, so the failed
	// submit is announced, not just drawn.
	if !strings.Contains(body, `data-hui-form-errors`) || !strings.Contains(body, `id="wd-form-errors"`) {
		t.Errorf("the validation summary or its focus hook is missing:\n%s", firstN(body, 600))
	}
	if wizardDemoLast() != nil {
		t.Errorf("a failed step-one submit recorded a payload")
	}

	// A malformed email keeps the reader on step one too.
	code, body = post("wizard_action=next&_step=0&wd-name=Ada+Lovelace&wd-email=not-an-email")
	if code != http.StatusOK {
		t.Fatalf("malformed email submit = %d, want 200", code)
	}
	if !strings.Contains(body, "Personal info") || !strings.Contains(body, "That email address does not look right.") {
		t.Errorf("a malformed email advanced or lost its message:\n%s", firstN(body, 600))
	}
	if wizardDemoLast() != nil {
		t.Errorf("a malformed-email submit recorded a payload")
	}

	// A valid step-one submit advances to Preferences with no summary.
	code, body = post("wizard_action=next&_step=0&wd-name=Ada+Lovelace&wd-email=ada@example.com")
	if code != http.StatusOK {
		t.Fatalf("valid submit = %d, want 200", code)
	}
	if !strings.Contains(body, "Preferences") {
		t.Errorf("a valid step-one submit did not advance:\n%s", firstN(body, 600))
	}
	if strings.Contains(body, "data-hui-form-errors") {
		t.Errorf("a valid submit carries the error surface:\n%s", firstN(body, 600))
	}

	// A post of the FINAL step carrying only the final field must not
	// confirm. The step number comes from the client, so trusting it
	// is how a reader skips the name and the email entirely; the
	// answer is the first step that is wrong, with its messages.
	code, body = post("wizard_action=next&_step=2&wd-comments=ok")
	if code != http.StatusOK {
		t.Fatalf("final submit without the earlier fields = %d, want 200", code)
	}
	if strings.Contains(body, "data-wizard-confirm") {
		t.Fatalf("a final-step post skipped the earlier required fields and confirmed:\n%s", firstN(body, 700))
	}
	for _, want := range []string{"Personal info", "Your full name is required.", "Your email is required."} {
		if !strings.Contains(body, want) {
			t.Errorf("the answer does not send the reader back to the first wrong step (%q missing):\n%s", want, firstN(body, 700))
		}
	}

	// Carrying every required field, the same final post confirms.
	code, body = post("wizard_action=next&_step=2&wd-name=Ada+Lovelace&wd-email=ada@example.com&wd-comments=ok")
	if code != http.StatusOK || !strings.Contains(body, "data-wizard-confirm") {
		t.Fatalf("a complete final submit = %d, want the confirmation page:\n%s", code, firstN(body, 700))
	}
}
