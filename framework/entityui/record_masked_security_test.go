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
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

var formTagRe = regexp.MustCompile(`(?s)<(/?)(form|input|select|textarea|option)\b([^>]*)>`)
var attrRe = regexp.MustCompile(`([a-zA-Z_:-]+)(?:="([^"]*)")?`)

var (
	simpleColorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	localDateTimeRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d{1,3})?)?$`)
)

// sanitizedValue is what the browser's value sanitization leaves of an
// input's value attribute: a color input holds a lowercase #rrggbb and
// turns anything else into #000000, and a datetime-local input blanks a
// value that is not a local date and time (a fraction past three
// digits included).
func sanitizedValue(typ, v string) string {
	switch typ {
	case "color":
		if simpleColorRe.MatchString(v) {
			return strings.ToLower(v)
		}
		return "#000000"
	case "datetime-local":
		if !localDateTimeRe.MatchString(v) {
			return ""
		}
	}
	return v
}

// formFields serializes the controls owned by the form with id formID the
// way FormData does: a control's owner is the form its form attribute
// names, else the form around it; disabled controls, unchecked boxes and
// nameless controls are skipped. A select submits its selected option
// (the last one marked, else the first that is not disabled), and
// nothing when that option is disabled.
func formFields(body, formID string) map[string]string {
	out := map[string]string{}
	current := ""
	var sel struct {
		name, owner, picked, first  string
		marked, pickedOff, hasFirst bool
	}
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
			if tag == "select" && sel.name != "" && sel.owner == formID {
				switch {
				case sel.marked && !sel.pickedOff:
					out[sel.name] = sel.picked
				case !sel.marked && sel.hasFirst:
					out[sel.name] = sel.first
				}
			}
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
			out[attrs["name"]] = sanitizedValue(attrs["type"], attrs["value"])
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
			sel.marked, sel.pickedOff, sel.hasFirst = false, false, false
			if attrs["name"] != "" && !disabled {
				sel.name, sel.owner = attrs["name"], owner
			}
		case "option":
			if sel.name == "" || sel.owner != formID {
				continue
			}
			if _, on := attrs["selected"]; on {
				sel.picked, sel.pickedOff, sel.marked = attrs["value"], disabled, true
			}
			if !disabled && !sel.hasFirst {
				sel.first, sel.hasFirst = attrs["value"], true
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
	return cell((&meta{}).rowValue(row, "token"))
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
	if !strings.Contains(body, i18nui.EntitySet.English()) {
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

// maskMemo installs an AfterGet hook that masks the queryable memo
// column (no NoQuery: the hook alone keeps its value off the API).
func maskMemo(t *testing.T, x *testUI) {
	t.Helper()
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil {
			p.Result["memo"] = "****"
		}
		return nil
	})
}

// The Activity tab never draws a value an AfterGet hook masks on the
// record: the audit log stored the column as written, and the JSON API
// answers the same caller "****" for it. A locked masked field is held
// back the same way as an editable one.
func TestActivityHidesHookMaskedField(t *testing.T) {
	for _, locked := range []bool{false, true} {
		ents := invoiceEntities()
		if locked {
			inv := ents["invoices"]
			d := *inv.Display
			d.Fields = map[string]entity.FieldDisplay{"memo": {Locked: true}}
			inv.Display = &d
			ents["invoices"] = inv
		}
		x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{
			"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
		}), withAudit(fakeAudit{entries: []AuditEntry{{
			At: time.Now(), Actor: "u1", Operation: "update",
			Before: map[string]any{"memo": "old-secret-memo", "issuedOn": "2026-01-05"},
			After:  map[string]any{"memo": "new-secret-memo", "issuedOn": "2026-02-09"},
		}}}))
		maskMemo(t, x)
		body := activityBody(t, x)
		if strings.Contains(body, "secret-memo") {
			t.Fatalf("SECURITY (locked=%v): the Activity tab drew a hook-masked value:\n%s", locked, body)
		}
		if !strings.Contains(body, `data-cui-internal="">Issued On</span>`) {
			t.Fatalf("locked=%v: an unmasked change is missing:\n%s", locked, body)
		}
	}
}
