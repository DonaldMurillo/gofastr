package admin

// The edit form sits between two failures that pull in opposite directions:
// prefilling from a hooked read writes the MASK back over the stored column,
// and prefilling from a raw read shows an admin a value the API masks. Both
// have shipped. These tests pin the resolution, masked columns render empty
// and write-only, plus the value formatting that broke on the way here.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func cardsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "cards",
		Fields: []schema.Field{
			{Name: "label", Type: schema.String, Required: true},
			{Name: "number", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false)
}

// maskNumber registers the documented redaction on both read surfaces.
func maskNumber(app *framework.App) {
	reg := app.HookRegistry("cards")
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		if _, ok := p.Result["number"]; ok {
			p.Result["number"] = "****1111"
		}
		return nil
	})
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		for i := range p.Results {
			if _, ok := p.Results[i]["number"]; ok {
				p.Results[i]["number"] = "****1111"
			}
		}
		return nil
	})
}

// The admin is the one reader who can see every row, so the raw prefill aimed
// the disclosure at the widest possible audience.
func TestEditFormDoesNotPrefillMaskedValue(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"cards": cardsConfig()})
	maskNumber(app)
	h := mountEntityAdmin(t, app, Config{Entities: []string{"cards"}}, testUser{"u1"})

	postForm(h, "/admin/e/cards/_create", url.Values{
		"label": {"Visa"}, "number": {"4111111111111111"},
	})
	id := firstID(t, db, "cards")

	rr := get(h, "/admin/e/cards/edit/"+id)
	if rr.Code != http.StatusOK {
		t.Fatalf("edit status %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: [admin] the edit form prefilled the stored value of a column the "+
			"API masks:\n%s", body)
	}
	if !strings.Contains(body, maskedHint) {
		t.Errorf("a masked column should render empty with the write-only hint %q; got:\n%s", maskedHint, body)
	}
	// The mask must not be prefilled either, submitting it would overwrite
	// the stored column with "****1111", which is the failure the raw read was
	// introduced to fix.
	if strings.Contains(body, `value="****1111"`) {
		t.Errorf("the edit form prefilled the MASK; pressing Save would persist it over the "+
			"stored value:\n%s", body)
	}
}

// Leaving a masked field blank keeps the stored value; typing into it replaces
// it. Without both halves the field is either unusable or destructive.
func TestEditFormMaskedFieldIsWriteOnly(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"cards": cardsConfig()})
	maskNumber(app)
	h := mountEntityAdmin(t, app, Config{Entities: []string{"cards"}}, testUser{"u1"})

	postForm(h, "/admin/e/cards/_create", url.Values{
		"label": {"Visa"}, "number": {"4111111111111111"},
	})
	id := firstID(t, db, "cards")

	// Edit the label, leave the masked field blank.
	postForm(h, "/admin/e/cards/_update/"+id, url.Values{
		"label": {"Visa Gold"}, "number": {""},
	})
	var label, number string
	if err := db.QueryRow(`SELECT label, number FROM cards WHERE id = ?`, id).Scan(&label, &number); err != nil {
		t.Fatal(err)
	}
	if label != "Visa Gold" {
		t.Errorf("label = %q, want the edit to have applied", label)
	}
	if number != "4111111111111111" {
		t.Errorf("number = %q; leaving a masked field blank must keep the stored value", number)
	}

	// Typing a new value replaces it.
	postForm(h, "/admin/e/cards/_update/"+id, url.Values{
		"label": {"Visa Gold"}, "number": {"4222222222222222"},
	})
	if err := db.QueryRow(`SELECT number FROM cards WHERE id = ?`, id).Scan(&number); err != nil {
		t.Fatal(err)
	}
	if number != "4222222222222222" {
		t.Errorf("number = %q; typing into a masked field must replace the stored value", number)
	}
}

// Multi-word on purpose. GetOne returns JSON casing (`cardNumber`) while the
// form looks columns up by schema name (`card_number`), so a masked-set keyed
// on the wrong side marks nothing masked, and only ever fails on multi-word
// columns, which a single-word fixture cannot show.
func TestEditFormMasksMultiWordColumn(t *testing.T) {
	db := newDB(t)
	cfg := entity.EntityConfig{
		Table: "wallets",
		Fields: []schema.Field{
			{Name: "owner_name", Type: schema.String, Required: true},
			{Name: "card_number", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"wallets": cfg})
	app.HookRegistry("wallets").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		// Keyed the way the entity's own endpoint returns it.
		if _, ok := p.Result["cardNumber"]; ok {
			p.Result["cardNumber"] = "****1111"
		}
		return nil
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"wallets"}}, testUser{"u1"})

	postForm(h, "/admin/e/wallets/_create", url.Values{
		"owner_name": {"Ada"}, "card_number": {"4111111111111111"},
	})
	id := firstID(t, db, "wallets")

	body := get(h, "/admin/e/wallets/edit/"+id).Body.String()
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: [admin] a multi-word masked column prefilled its stored value:\n%s", body)
	}
	if !strings.Contains(body, `value="Ada"`) {
		t.Errorf("the unmasked column should still prefill:\n%s", body)
	}
}

// An unmasked column still prefills, or the form is useless.
func TestEditFormPrefillsUnmaskedValues(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"cards": cardsConfig()})
	maskNumber(app)
	h := mountEntityAdmin(t, app, Config{Entities: []string{"cards"}}, testUser{"u1"})

	postForm(h, "/admin/e/cards/_create", url.Values{"label": {"Visa"}, "number": {"4111111111111111"}})
	id := firstID(t, db, "cards")

	body := get(h, "/admin/e/cards/edit/"+id).Body.String()
	if !strings.Contains(body, `value="Visa"`) {
		t.Errorf("an unmasked column must still prefill:\n%s", body)
	}
}

// getRowForEdit reads through the in-process API, which hands back what the
// driver scanned, a time.Time for date/timestamp columns, where the old JSON
// round-trip produced a string. fmt.Sprint renders Go's default layout, the
// validator rejects it on submit, and the user's other edits go with it.
func TestEditFormRendersTimestampsAsRFC3339(t *testing.T) {
	db := newDB(t)
	cfg := entity.EntityConfig{
		Table: "events",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "starts_at", Type: schema.Timestamp},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"events": cfg})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"events"}}, testUser{"u1"})

	when := "2026-01-02T03:04:05Z"
	postForm(h, "/admin/e/events/_create", url.Values{"name": {"Launch"}, "starts_at": {when}})
	id := firstID(t, db, "events")

	body := get(h, "/admin/e/events/edit/"+id).Body.String()
	if strings.Contains(body, "+0000 UTC") {
		t.Fatalf("the edit form rendered Go's default time layout, which fails RFC 3339 "+
			"validation on submit:\n%s", body)
	}

	// The round trip is the real assertion: re-submit the form as rendered,
	// changing only the name, and check both fields survived.
	postForm(h, "/admin/e/events/_update/"+id, url.Values{
		"name": {"Launch v2"}, "starts_at": {formValue(body, "starts_at")},
	})
	var name string
	var starts time.Time
	if err := db.QueryRow(`SELECT name, starts_at FROM events WHERE id = ?`, id).Scan(&name, &starts); err != nil {
		t.Fatal(err)
	}
	if name != "Launch v2" {
		t.Errorf("name = %q; the edit was rejected and the user's change was lost", name)
	}
	if starts.UTC().Format(time.RFC3339) != when {
		t.Errorf("starts_at = %q, want %q", starts.UTC().Format(time.RFC3339), when)
	}
}

// formValue pulls an input's rendered value= out of the page, so the re-submit
// posts exactly what a browser would.
func formValue(body, name string) string {
	marker := `name="` + name + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	j := strings.Index(rest, `value="`)
	if j < 0 {
		return ""
	}
	rest = rest[j+len(`value="`):]
	k := strings.Index(rest, `"`)
	if k < 0 {
		return ""
	}
	return rest[:k]
}

// A checkbox cannot express "unchanged", unchecked and absent look identical,
// and formToJSON emitted a bool either way, so a masked bool was written
// back as false on EVERY save, including one that touched only another field.
// is_admin / is_active / published are exactly the columns an app masks.
func TestMaskedBoolIsNotClobberedOnSave(t *testing.T) {
	db := newDB(t)
	cfg := entity.EntityConfig{
		Table: "accts",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "is_admin", Type: schema.Bool},
			{Name: "tier", Type: schema.Enum, Values: []string{"free", "gold"}, Default: "free"},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"accts": cfg})
	app.HookRegistry("accts").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		p.Result["isAdmin"] = false // the mask
		p.Result["tier"] = "hidden"
		return nil
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"accts"}}, testUser{"u1"})

	postForm(h, "/admin/e/accts/_create", url.Values{
		"name": {"root"}, "is_admin": {"on"}, "tier": {"gold"},
	})
	id := firstID(t, db, "accts")
	var isAdmin bool
	var tier string
	if err := db.QueryRow(`SELECT is_admin, tier FROM accts WHERE id=?`, id).Scan(&isAdmin, &tier); err != nil {
		t.Fatal(err)
	}
	if !isAdmin || tier != "gold" {
		t.Fatalf("precondition: stored is_admin=%v tier=%q", isAdmin, tier)
	}

	// The form must offer an explicit "unchanged" choice rather than an
	// unchecked box that silently means false.
	body := get(h, "/admin/e/accts/edit/"+id).Body.String()
	if !strings.Contains(body, maskedUnchanged) {
		t.Errorf("a masked bool/enum must render an explicit %q option; got:\n%s", maskedUnchanged, body)
	}

	// Edit only the name, submitting blanks for the masked columns exactly as
	// the rendered form would.
	postForm(h, "/admin/e/accts/_update/"+id, url.Values{
		"name": {"root2"}, "is_admin": {""}, "tier": {""},
	})
	if err := db.QueryRow(`SELECT is_admin, tier FROM accts WHERE id=?`, id).Scan(&isAdmin, &tier); err != nil {
		t.Fatal(err)
	}
	if !isAdmin {
		t.Errorf("SECURITY/DATA LOSS: a masked bool was cleared to false by an unrelated edit")
	}
	if tier != "gold" {
		t.Errorf("a masked enum was overwritten with %q by an unrelated edit", tier)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM accts WHERE id=?`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "root2" {
		t.Errorf("the actual edit did not apply: name=%q", name)
	}

	// And an explicit choice still writes.
	postForm(h, "/admin/e/accts/_update/"+id, url.Values{
		"name": {"root2"}, "is_admin": {"false"}, "tier": {"free"},
	})
	if err := db.QueryRow(`SELECT is_admin, tier FROM accts WHERE id=?`, id).Scan(&isAdmin, &tier); err != nil {
		t.Fatal(err)
	}
	if isAdmin || tier != "free" {
		t.Errorf("an explicit value on a masked field must write: is_admin=%v tier=%q", isAdmin, tier)
	}
}

// <input type="date"> accepts only yyyy-mm-dd and blanks itself on anything
// else, so an RFC 3339 value round-tripped through the form came back empty
// and the save wiped the column.
func TestEditFormRendersDateAsDateInputValue(t *testing.T) {
	db := newDB(t)
	cfg := entity.EntityConfig{
		Table: "tasks",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "due_on", Type: schema.Date},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"tasks": cfg})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"tasks"}}, testUser{"u1"})

	postForm(h, "/admin/e/tasks/_create", url.Values{"title": {"Ship"}, "due_on": {"2026-03-04"}})
	id := firstID(t, db, "tasks")

	body := get(h, "/admin/e/tasks/edit/"+id).Body.String()
	if got := formValue(body, "due_on"); got != "2026-03-04" {
		t.Fatalf("date input value = %q, want 2026-03-04; anything else is blanked by the "+
			"browser and the save wipes the column", got)
	}
}

// A failing AfterGet means we cannot prove any column is safe to prefill, so
// the form must fail closed rather than fall back to raw values.
func TestEditFormFailsClosedWhenTheHookErrors(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"cards": cardsConfig()})
	app.HookRegistry("cards").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"cards"}}, testUser{"u1"})

	postForm(h, "/admin/e/cards/_create", url.Values{"label": {"Visa"}, "number": {"4111111111111111"}})
	id := firstID(t, db, "cards")

	body := get(h, "/admin/e/cards/edit/"+id).Body.String()
	if strings.Contains(body, "4111111111111111") || strings.Contains(body, `value="Visa"`) {
		t.Errorf("SECURITY: the redactor failed and the form prefilled stored values anyway:\n%s", body)
	}
	if !strings.Contains(body, maskedHint) {
		t.Errorf("every field should render write-only when the redactor cannot be run:\n%s", body)
	}

	// And saving that all-blank form must not wipe the record.
	postForm(h, "/admin/e/cards/_update/"+id, url.Values{"label": {""}, "number": {""}})
	var label, number string
	if err := db.QueryRow(`SELECT label, number FROM cards WHERE id=?`, id).Scan(&label, &number); err != nil {
		t.Fatal(err)
	}
	if label != "Visa" || number != "4111111111111111" {
		t.Errorf("the fail-closed form wiped the record on save: label=%q number=%q", label, number)
	}
}

// cellText's nullable-timestamp branch: nil must render empty, not "<nil>".
func TestCellTextHandlesNilTimePointer(t *testing.T) {
	var p *time.Time
	if got := cellText(p); got != "" {
		t.Errorf("cellText(nil *time.Time) = %q, want \"\" — %q lands in the input and fails "+
			"validation on submit", got, got)
	}
	now := time.Unix(0, 0).UTC()
	if got := cellText(&now); got != now.Format(time.RFC3339) {
		t.Errorf("cellText(*time.Time) = %q, want RFC 3339", got)
	}
}

// controlFor extracts the rendered control carrying name="<field>", so an
// assertion cannot be satisfied by a DIFFERENT masked field elsewhere on the
// page, which is how the first masked-bool test passed with the bool branch
// deleted (its sibling enum emitted the same marker).
func controlFor(body, field string) string {
	marker := `name="` + field + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	// Walk back to the opening tag, forward to the end of the control.
	start := strings.LastIndex(body[:i], "<")
	if start < 0 {
		start = i
	}
	rest := body[start:]
	for _, close := range []string{"</select>", "</textarea>", ">"} {
		if j := strings.Index(rest, close); j >= 0 {
			return rest[:j+len(close)]
		}
	}
	return rest
}

// A masked bool must render as a three-state control. A checkbox cannot say
// "unchanged": unchecked and absent are the same bytes, so the column could
// never be set to false, and the form would display "off" for a stored true.
func TestMaskedBoolRendersAThreeStateControl(t *testing.T) {
	db := newDB(t)
	cfg := entity.EntityConfig{
		Table: "flags",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "is_admin", Type: schema.Bool},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"flags": cfg})
	app.HookRegistry("flags").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		p.Result["isAdmin"] = false
		return nil
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"flags"}}, testUser{"u1"})

	postForm(h, "/admin/e/flags/_create", url.Values{"name": {"root"}, "is_admin": {"on"}})
	id := firstID(t, db, "flags")

	ctrl := controlFor(get(h, "/admin/e/flags/edit/"+id).Body.String(), "is_admin")
	if ctrl == "" {
		t.Fatal("no control rendered for is_admin")
	}
	if strings.Contains(ctrl, `type="checkbox"`) {
		t.Errorf("a masked bool rendered as a checkbox, which cannot express \"unchanged\": %s", ctrl)
	}
	if !strings.Contains(ctrl, maskedUnchanged) {
		t.Errorf("the is_admin control itself must carry the %q option: %s", maskedUnchanged, ctrl)
	}
	// And it must be able to set false explicitly.
	postForm(h, "/admin/e/flags/_update/"+id, url.Values{"name": {"root"}, "is_admin": {"false"}})
	var isAdmin bool
	if err := db.QueryRow(`SELECT is_admin FROM flags WHERE id=?`, id).Scan(&isAdmin); err != nil {
		t.Fatal(err)
	}
	if isAdmin {
		t.Error("a masked bool could not be set to false")
	}
}

// A REQUIRED masked enum or relation picker is where the blank placeholder is
// load-bearing: without it nothing is preselected, the browser posts the first
// option, and the stored value is silently overwritten. It is also the only
// case where `required` on a blank control would block every submit.
func TestRequiredMaskedSelectsOfferUnchangedAndDropRequired(t *testing.T) {
	db := newDB(t)
	if _, err := db.Exec(`CREATE TABLE owners (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	cfg := entity.EntityConfig{
		Table: "items",
		Fields: []schema.Field{
			{Name: "label", Type: schema.String, Required: true},
			{Name: "tier", Type: schema.Enum, Values: []string{"free", "gold"}, Required: true, Default: "free"},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"items": cfg})
	app.HookRegistry("items").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		p.Result["tier"] = "hidden"
		return nil
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"items"}}, testUser{"u1"})

	postForm(h, "/admin/e/items/_create", url.Values{"label": {"A"}, "tier": {"gold"}})
	id := firstID(t, db, "items")

	ctrl := controlFor(get(h, "/admin/e/items/edit/"+id).Body.String(), "tier")
	if ctrl == "" {
		t.Fatal("no control rendered for tier")
	}
	if !strings.Contains(ctrl, `value=""`) {
		t.Errorf("a required masked enum must offer a blank option, or the browser posts the "+
			"first one and overwrites the stored value: %s", ctrl)
	}
	if strings.Contains(ctrl, "required") {
		t.Errorf("a masked control cannot be required — there is nothing to submit when the "+
			"intent is to leave it alone: %s", ctrl)
	}

	// Submitting the form as rendered leaves the stored value alone.
	postForm(h, "/admin/e/items/_update/"+id, url.Values{"label": {"A2"}, "tier": {""}})
	var tier string
	if err := db.QueryRow(`SELECT tier FROM items WHERE id=?`, id).Scan(&tier); err != nil {
		t.Fatal(err)
	}
	if tier != "gold" {
		t.Errorf("tier = %q; a blank required masked enum must keep the stored value", tier)
	}
}

// ----- mass-assignment whitelist ------------------------------------------
//
// formToJSON iterates the schema's editable field set (editableFields skips
// Hidden / ReadOnly / AutoGenerate), never r.PostForm, so a crafted POST
// cannot smuggle server-owned columns into the JSON body callCrud forwards.
// Sister of the masked-column pins above — both are the form layer deciding
// what a POST may write. The CrudHandler independently strips non-writable
// columns (verified by sabotage: with the form whitelist disabled, evil
// values still never reach the row), so the shapes below pin the composed
// property while TestFormToJSONDropsNonEditableKeys bites at the form layer
// itself.

func secretsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "secrets",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "password_hash", Type: schema.String, Hidden: true},
			{Name: "created_by", Type: schema.String, ReadOnly: true},
			{Name: "ref", Type: schema.String, AutoGenerate: schema.AutoUUID},
		},
	}.WithTimestamps(false)
}

// A POST carrying a Hidden column, a ReadOnly column, an AutoGenerate column
// and a nonexistent column creates the row with none of them: the hidden
// column stays NULL, the autogen column is server-produced, and the unknown
// key never reaches the CrudHandler's strict body parse (which would reject
// the whole create and leave no row at all).
func TestEntitySaveIgnoresNonEditableFields(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"secrets": secretsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"secrets"}}, testUser{"u1"})

	rr := postForm(h, "/admin/e/secrets/_create", url.Values{
		"title":         {"Whitelisted"},
		"password_hash": {"evil-hash"},
		"created_by":    {"evil-actor"},
		"ref":           {"evil-ref"},
		"nonexistent":   {"evil-column"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create should 303; got %d body=%s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/e/secrets" {
		t.Fatalf("create Location = %q, want /admin/e/secrets (a non-list Location means the CrudHandler rejected the body)", loc)
	}

	var hash, actor, ref string
	err := db.QueryRow(`SELECT COALESCE(password_hash,''), COALESCE(created_by,''), COALESCE(ref,'') FROM secrets WHERE title='Whitelisted'`).Scan(&hash, &actor, &ref)
	if err != nil {
		t.Fatalf("row not created: %v", err)
	}
	if hash != "" {
		t.Errorf("SECURITY: [mass-assignment] posted password_hash (Hidden) persisted as %q", hash)
	}
	if actor != "" {
		t.Errorf("SECURITY: [mass-assignment] posted created_by (ReadOnly) persisted as %q", actor)
	}
	if ref == "evil-ref" {
		t.Errorf("SECURITY: [mass-assignment] posted ref (AutoGenerate) persisted verbatim: %q", ref)
	}
}

// Same property on the update leg, with stored baselines the POST must not
// touch.
func TestEntityUpdateIgnoresNonEditableFields(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"secrets": secretsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"secrets"}}, testUser{"u1"})

	postForm(h, "/admin/e/secrets/_create", url.Values{"title": {"Before"}})
	id := firstID(t, db, "secrets")
	if _, err := db.Exec(`UPDATE secrets SET password_hash='stored-hash', created_by='system', ref='stored-ref' WHERE id=?`, id); err != nil {
		t.Fatalf("seed baselines: %v", err)
	}

	rr := postForm(h, "/admin/e/secrets/_update/"+id, url.Values{
		"title":         {"After"},
		"password_hash": {"evil-hash"},
		"created_by":    {"evil-actor"},
		"ref":           {"evil-ref"},
		"nonexistent":   {"evil-column"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("update should 303; got %d body=%s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/e/secrets" {
		t.Fatalf("update Location = %q, want /admin/e/secrets (a non-list Location means the CrudHandler rejected the body)", loc)
	}

	var title, hash, actor, ref string
	err := db.QueryRow(`SELECT title, COALESCE(password_hash,''), COALESCE(created_by,''), COALESCE(ref,'') FROM secrets WHERE id=?`, id).Scan(&title, &hash, &actor, &ref)
	if err != nil {
		t.Fatalf("row vanished: %v", err)
	}
	if title != "After" {
		t.Errorf("legitimate edit did not persist; title=%q", title)
	}
	if hash != "stored-hash" || actor != "system" || ref != "stored-ref" {
		t.Errorf("SECURITY: [mass-assignment] update overwrote protected columns: hash=%q created_by=%q ref=%q", hash, actor, ref)
	}
}

// TestFormToJSONDropsNonEditableKeys is the form-layer half of the pin: the
// JSON body callCrud forwards carries ONLY editable keys, even when the POST
// carries Hidden / ReadOnly / AutoGenerate / unknown keys. The end-to-end
// shapes above cannot catch a formToJSON refactor to iterating r.PostForm
// because the CrudHandler strips non-writable columns anyway; this one can.
func TestFormToJSONDropsNonEditableKeys(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"secrets": secretsConfig()})
	ent, err := app.Registry.Get("secrets")
	if err != nil {
		t.Fatalf("registry get: %v", err)
	}

	r, err := http.NewRequest(http.MethodPost, "/admin/e/secrets/_create", strings.NewReader(url.Values{
		"title":         {"Unit"},
		"password_hash": {"evil-hash"},
		"created_by":    {"evil-actor"},
		"ref":           {"evil-ref"},
		"nonexistent":   {"evil-column"},
	}.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	body, fieldErrs := formToJSON(ent, r, map[string]bool{})
	if len(fieldErrs) != 0 {
		t.Fatalf("unexpected field errors: %v", fieldErrs)
	}
	// Single-key map marshals deterministically; anything else in the body
	// is a smuggled column.
	if body != `{"title":"Unit"}` {
		t.Errorf("SECURITY: [mass-assignment] form body = %s, want exactly {\"title\":\"Unit\"}", body)
	}
}
