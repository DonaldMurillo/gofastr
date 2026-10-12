package entityui

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

var formTagRe = regexp.MustCompile(`(?s)<(/?)(form|input|select|textarea|option)\b([^>]*)>`)
var attrRe = regexp.MustCompile(`([a-zA-Z_:-]+)(?:="([^"]*)")?`)

// formFields serializes the controls owned by the form with id formID the
// way FormData does: a control's owner is the form its form attribute
// names, else the form around it; disabled controls, unchecked boxes and
// nameless controls are skipped.
func formFields(body, formID string) map[string]string {
	out := map[string]string{}
	current := ""
	var sel struct{ name, owner string }
	matches := formTagRe.FindAllStringSubmatchIndex(body, -1)
	for i, m := range matches {
		closing, tag, raw := body[m[2]:m[3]], body[m[4]:m[5]], body[m[6]:m[7]]
		attrs := map[string]string{}
		for _, a := range attrRe.FindAllStringSubmatch(raw, -1) {
			attrs[a[1]] = html.UnescapeString(a[2])
		}
		if tag == "form" {
			if closing == "" {
				current = attrs["id"]
			} else {
				current = ""
			}
			continue
		}
		if closing != "" {
			if tag == "select" {
				sel.name = ""
			}
			continue
		}
		owner := current
		if f, ok := attrs["form"]; ok {
			owner = f
		}
		_, disabled := attrs["disabled"]
		switch tag {
		case "input":
			if attrs["name"] == "" || disabled || owner != formID {
				continue
			}
			if t := attrs["type"]; t == "checkbox" || t == "radio" {
				if _, on := attrs["checked"]; !on {
					continue
				}
			}
			out[attrs["name"]] = attrs["value"]
		case "textarea":
			if attrs["name"] == "" || disabled || owner != formID {
				continue
			}
			end := len(body)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			out[attrs["name"]] = html.UnescapeString(body[m[1]:end])
		case "select":
			sel.name, sel.owner = "", ""
			if attrs["name"] != "" && !disabled {
				sel.name, sel.owner = attrs["name"], owner
				if owner == formID {
					out[sel.name] = ""
				}
			}
		case "option":
			if sel.name == "" || sel.owner != formID {
				continue
			}
			if _, on := attrs["selected"]; on {
				out[sel.name] = attrs["value"]
			}
		}
	}
	return out
}

func maskToken(t *testing.T, x *testUI) {
	t.Helper()
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil {
			p.Result["token"] = "****"
		}
		return nil
	})
}

// putInvoice sends body to the invoice's PUT route as u1, the request
// the runtime makes when a form submits, and returns the status.
func putInvoice(t *testing.T, x *testUI, fields map[string]string) int {
	t.Helper()
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fields)
	req := httptest.NewRequest(http.MethodPut, "/api/invoices/inv-1", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "inv-1")
	req = req.WithContext(asUser(req.Context(), "u1"))
	rec := httptest.NewRecorder()
	ch.Update().ServeHTTP(rec, req)
	return rec.Code
}

func storedToken(t *testing.T, x *testUI) string {
	t.Helper()
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	row, err := ch.GetOne(asUser(context.Background(), "u1"), "inv-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	return cell(rowValue(row, "token"))
}

// Saving the record form with a hook-masked text field leaves the
// stored value alone: the record form does not carry the field, so an
// edit to another field cannot write a blank over it.
func TestMaskedTextSurvivesUnrelatedSave(t *testing.T) {
	x := newInvoiceUI(t)
	maskToken(t, x)
	body := renderRecord(t, x, "inv-1", nil)
	if strings.Contains(body, "tok-secret") {
		t.Fatalf("SECURITY: the masked field rendered its stored value:\n%s", body)
	}
	fields := formFields(body, "eui-invoices-form")
	if _, ok := fields["number"]; !ok {
		t.Fatalf("the record form lost its editable fields: %v", fields)
	}
	if _, ok := fields["token"]; ok {
		t.Fatalf("SECURITY: the record form submits the masked field: %v", fields)
	}
	fields["number"] = "INV-1b"
	if code := putInvoice(t, x, fields); code != http.StatusOK {
		t.Fatalf("save = %d", code)
	}
	if got := storedToken(t, x); got != "tok-secret" {
		t.Fatalf("SECURITY: an unrelated save rewrote the masked field to %q", got)
	}
	if !strings.Contains(body, i18nui.Defaults[i18nui.KeyEntitySet]) {
		t.Fatalf("a set masked field says so:\n%s", body)
	}
}

// The masked field changes through its own Replace form, which carries
// that one field, required, and PUTs it to the record.
func TestMaskedTextReplaceForm(t *testing.T) {
	x := newInvoiceUI(t)
	maskToken(t, x)
	body := renderRecord(t, x, "inv-1", nil)
	const id = "eui-invoices-replace-token"
	if !strings.Contains(body, `id="`+id+`"`) || !strings.Contains(body, `data-cui-rpc-method="PUT"`) {
		t.Fatalf("no Replace form for the masked field:\n%s", body)
	}
	fields := formFields(body, id)
	delete(fields, "_csrf")
	if v, ok := fields["token"]; len(fields) != 1 || !ok || v != "" {
		t.Fatalf("the Replace form carries %v, want only a blank token", fields)
	}
	if !regexp.MustCompile(`<input[^>]*name="token"[^>]*required`).MatchString(body) &&
		!regexp.MustCompile(`<input[^>]*required[^>]*name="token"`).MatchString(body) {
		t.Fatalf("the Replace input is not required, so a blank could submit:\n%s", body)
	}
	fields["token"] = "tok-new"
	if code := putInvoice(t, x, fields); code != http.StatusOK {
		t.Fatalf("replace = %d", code)
	}
	if got := storedToken(t, x); got != "tok-new" {
		t.Fatalf("the Replace form did not write the new value: %q", got)
	}
}
