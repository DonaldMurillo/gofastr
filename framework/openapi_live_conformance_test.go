package framework

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// specCheck validates v against an OpenAPI 3.1 schema node, resolving local
// $refs against doc. It returns "" when v conforms, else the first JSON
// pointer that does not, with the reason. It covers the keywords the
// framework's generated documents use: type (string or list), enum, const,
// required, properties, additionalProperties, items, maxItems, oneOf, allOf,
// anyOf, format (date-time, date, uuid), minLength/maxLength, minimum/maximum
// and maxProperties.
func specCheck(doc, s map[string]any, v any, at string) string {
	if ref, ok := s["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		target, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)[name].(map[string]any)
		if !ok {
			return at + ": unresolvable $ref " + ref
		}
		return specCheck(doc, target, v, at)
	}
	if typ, ok := s["type"]; ok && !jsonTypeIs(typ, v) {
		return fmt.Sprintf("%s: %#v is not of type %v", at, v, typ)
	}
	if enum, ok := s["enum"].([]any); ok && !slices.Contains(enum, v) {
		return fmt.Sprintf("%s: %#v is not in enum %v", at, v, enum)
	}
	if one, ok := s["oneOf"].([]any); ok {
		n := 0
		var why []string
		for i, alt := range one {
			if msg := specCheck(doc, alt.(map[string]any), v, fmt.Sprintf("%s(oneOf[%d])", at, i)); msg == "" {
				n++
			} else {
				why = append(why, msg)
			}
		}
		if n != 1 {
			return fmt.Sprintf("%s: matches %d oneOf alternatives, want 1 (%s)", at, n, strings.Join(why, "; "))
		}
	}
	if c, ok := s["const"]; ok && c != v {
		return fmt.Sprintf("%s: %#v is not the const %#v", at, v, c)
	}
	if anyOf, ok := s["anyOf"].([]any); ok {
		var why []string
		for i, alt := range anyOf {
			msg := specCheck(doc, alt.(map[string]any), v, fmt.Sprintf("%s(anyOf[%d])", at, i))
			if msg == "" {
				why = nil
				break
			}
			why = append(why, msg)
		}
		if why != nil {
			return fmt.Sprintf("%s: matches no anyOf alternative (%s)", at, strings.Join(why, "; "))
		}
	}
	if all, ok := s["allOf"].([]any); ok {
		for i, part := range all {
			if msg := specCheck(doc, part.(map[string]any), v, fmt.Sprintf("%s(allOf[%d])", at, i)); msg != "" {
				return msg
			}
		}
	}
	if str, ok := v.(string); ok {
		switch s["format"] {
		case "date-time":
			if _, err := time.Parse(time.RFC3339, str); err != nil {
				return fmt.Sprintf("%s: %q is not an RFC 3339 date-time", at, str)
			}
		case "date":
			if _, err := time.Parse("2006-01-02", str); err != nil {
				return fmt.Sprintf("%s: %q is not a full-date", at, str)
			}
		case "uuid":
			if !regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`).MatchString(str) {
				return fmt.Sprintf("%s: %q is not a uuid", at, str)
			}
		}
		if n, ok := s["maxLength"].(float64); ok && float64(len([]rune(str))) > n {
			return fmt.Sprintf("%s: %q is longer than %v", at, str, n)
		}
		if n, ok := s["minLength"].(float64); ok && float64(len([]rune(str))) < n {
			return fmt.Sprintf("%s: %q is shorter than %v", at, str, n)
		}
	}
	if f, ok := v.(float64); ok {
		if n, ok := s["minimum"].(float64); ok && f < n {
			return fmt.Sprintf("%s: %v < minimum %v", at, f, n)
		}
		if n, ok := s["maximum"].(float64); ok && f > n {
			return fmt.Sprintf("%s: %v > maximum %v", at, f, n)
		}
	}
	if obj, isObj := v.(map[string]any); isObj {
		if req, ok := s["required"].([]any); ok {
			for _, k := range req {
				if _, ok := obj[k.(string)]; !ok {
					return fmt.Sprintf("%s: required key %q missing (have %v)", at, k, specKeys(obj))
				}
			}
		}
		props, _ := s["properties"].(map[string]any)
		for k, pv := range obj {
			if ps, ok := props[k].(map[string]any); ok {
				if msg := specCheck(doc, ps, pv, at+"/"+k); msg != "" {
					return msg
				}
				continue
			}
			switch ap := s["additionalProperties"].(type) {
			case bool:
				if !ap {
					return fmt.Sprintf("%s: key %q is not allowed", at, k)
				}
			case map[string]any:
				if msg := specCheck(doc, ap, pv, at+"/"+k); msg != "" {
					return msg
				}
			}
		}
		if n, ok := s["maxProperties"].(float64); ok && float64(len(obj)) > n {
			return fmt.Sprintf("%s: more than %v properties", at, n)
		}
	}
	if arr, ok := v.([]any); ok {
		if items, ok := s["items"].(map[string]any); ok {
			for i, it := range arr {
				if msg := specCheck(doc, items, it, fmt.Sprintf("%s/%d", at, i)); msg != "" {
					return msg
				}
			}
		}
		if n, ok := s["maxItems"].(float64); ok && float64(len(arr)) > n {
			return fmt.Sprintf("%s: more than %v items", at, n)
		}
	}
	return ""
}

func specKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// specOp returns the operation node documented at path+method.
func specOp(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	item, ok := doc["paths"].(map[string]any)[path].(map[string]any)
	if !ok {
		t.Fatalf("spec documents no path %s (have %v)", path, specKeys(doc["paths"].(map[string]any)))
	}
	op, ok := item[method].(map[string]any)
	if !ok {
		t.Fatalf("spec documents no %s %s", strings.ToUpper(method), path)
	}
	return op
}

// conformsResponse asserts the live response matches the operation's
// documented response for its status: the status is documented, and a JSON
// body conforms to the declared schema.
func conformsResponse(t *testing.T, doc map[string]any, path, method string, resp *TestResponse) {
	t.Helper()
	op := specOp(t, doc, path, method)
	status := fmt.Sprint(resp.Status())
	r, ok := op["responses"].(map[string]any)[status].(map[string]any)
	if !ok {
		t.Errorf("%s %s answered %s, which the spec does not document (documented: %v); body %s",
			strings.ToUpper(method), path, status, specKeys(op["responses"].(map[string]any)), resp.Body())
		return
	}
	content, _ := r["content"].(map[string]any)
	if content == nil {
		if strings.TrimSpace(resp.Body()) != "" {
			t.Errorf("%s %s %s: spec declares no body, got %s", strings.ToUpper(method), path, status, resp.Body())
		}
		return
	}
	media, ok := content["application/json"].(map[string]any)
	if !ok {
		return
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader([]byte(resp.Body())))
	if err := dec.Decode(&v); err != nil {
		t.Errorf("%s %s %s: body is not JSON: %v: %s", strings.ToUpper(method), path, status, err, resp.Body())
		return
	}
	if msg := specCheck(doc, media["schema"].(map[string]any), v, ""); msg != "" {
		t.Errorf("%s %s %s: response does not conform to the spec: %s\nbody: %s", strings.ToUpper(method), path, status, msg, resp.Body())
	}
}

// servedSpecApp migrates app's registry, starts it, and returns the
// /openapi.json it serves (the app must set WithPublicOpenAPI).
func servedSpecApp(t *testing.T, db *sql.DB, app *App) map[string]any {
	t.Helper()
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(covStartAndStop(t, app))
	var doc map[string]any
	if err := TestHarness(t, app).Get("/openapi.json").JSON(&doc); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	return doc
}

// A snake_case app's routes send each field under its column name, and
// the document named them in camelCase: every response failed its own
// schema's required list, and a client built from it sent keys the
// routes do not read.
func TestOpenAPI_SnakeCaseAppDocumentsColumnKeys(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithConfig(AppConfig{JSONCase: CaseSnake}), WithDB(db), WithPublicOpenAPI())
		app.Entity("widgets", entity.EntityConfig{
			Table: "widgets",
			Fields: []schema.Field{
				{Name: "display_name", Type: schema.String, Required: true},
				{Name: "alias_col", Type: schema.String, WireName: "aliasKey"},
			},
		})
		doc := servedSpecApp(t, db, app)
		props := doc["components"].(map[string]any)["schemas"].(map[string]any)["widgets"].(map[string]any)["properties"].(map[string]any)
		for _, k := range []string{"display_name", "created_at", "aliasKey"} {
			if _, ok := props[k]; !ok {
				t.Errorf("snake_case document has no %q property: %v", k, specKeys(props))
			}
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		created := ta.Post("/widgets", map[string]any{"display_name": "w", "aliasKey": "a"})
		created.AssertStatus(t, http.StatusCreated)
		conformsResponse(t, doc, "/widgets", "post", created)
		var got map[string]any
		_ = created.JSON(&got)
		id, _ := got["data"].(map[string]any)["id"].(string)
		conformsResponse(t, doc, "/widgets/{id}", "get", ta.Get("/widgets/"+id))
	})
}

// A relation field holds the key of the record it names. The document
// typed every one as a uuid string, so a foreign key to an integer key
// (stored and answered as a JSON number) failed every read, and a client
// built from the document sent a string the create refuses; a key to a
// text slug carried a uuid format no value of it meets.
func TestOpenAPI_RelationFieldsTypedByTargetKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithPublicOpenAPI())
		app.Entity("counters", entity.EntityConfig{
			Table: "counters",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "label", Type: schema.String, Required: true},
			},
		})
		app.Entity("slugs", entity.EntityConfig{
			Table: "slugs",
			Fields: []schema.Field{
				{Name: "id", Type: schema.String, Required: true},
				{Name: "label", Type: schema.String},
			},
		})
		app.Entity("people", entity.EntityConfig{
			Table:  "people",
			Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		})
		app.Entity("gadgets", entity.EntityConfig{
			Table: "gadgets",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "counter_id", Type: schema.Relation, To: "counters"},
				{Name: "slug_id", Type: schema.Relation, To: "slugs"},
				{Name: "owner_id", Type: schema.Relation, To: "people"},
			},
		})
		doc := servedSpecApp(t, db, app)
		props := doc["components"].(map[string]any)["schemas"].(map[string]any)["gadgets"].(map[string]any)["properties"].(map[string]any)
		if p := props["ownerId"].(map[string]any); p["format"] != "uuid" {
			t.Errorf("a relation to a uuid key lost its format: %v", p)
		}
		create := specOp(t, doc, "/gadgets", "post")["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)

		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		var counter, person map[string]any
		_ = ta.Post("/counters", map[string]any{"label": "c"}).JSON(&counter)
		_ = ta.Post("/people", map[string]any{"name": "p"}).JSON(&person)
		ta.Post("/slugs", map[string]any{"id": "intro", "label": "s"}).AssertStatus(t, http.StatusCreated)
		body := map[string]any{
			"title":     "g",
			"counterId": counter["data"].(map[string]any)["id"],
			"slugId":    "intro",
			"ownerId":   person["data"].(map[string]any)["id"],
		}
		var asJSON any
		raw, _ := json.Marshal(body)
		_ = json.Unmarshal(raw, &asJSON)
		if msg := specCheck(doc, create, asJSON, ""); msg != "" {
			t.Errorf("the create schema refuses a body the route accepts: %s", msg)
		}
		created := ta.Post("/gadgets", body)
		created.AssertStatus(t, http.StatusCreated)
		conformsResponse(t, doc, "/gadgets", "post", created)
		var got map[string]any
		_ = created.JSON(&got)
		id, _ := got["data"].(map[string]any)["id"].(string)
		conformsResponse(t, doc, "/gadgets/{id}", "get", ta.Get("/gadgets/"+id))

		// The filter parser reads the integer foreign key as an Int column:
		// every operator the document advertises on it is one it takes.
		advertised := 0
		for _, p := range specOp(t, doc, "/gadgets", "get")["parameters"].([]any) {
			name := p.(map[string]any)["name"].(string)
			if !strings.HasPrefix(name, "counter_id") {
				continue
			}
			advertised++
			if name == "counter_id" {
				if typ := p.(map[string]any)["schema"].(map[string]any)["type"]; typ != "integer" {
					t.Errorf("?counter_id= is documented as %v; the parser refuses anything but an integer", typ)
				}
			}
			resp := ta.Get("/gadgets?" + name + "=1")
			if resp.Status() != http.StatusOK {
				t.Errorf("advertised filter ?%s=1 answered %d: %s", name, resp.Status(), resp.Body())
			}
		}
		if advertised == 0 {
			t.Fatal("the list documents no counter_id filter")
		}
	})
}
