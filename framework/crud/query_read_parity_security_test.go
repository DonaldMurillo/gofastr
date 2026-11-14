package crud

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"context"
	"encoding/json"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// One table owns every HTTP query-reading surface. A new read surface joins
// here so it inherits the full single-value and strict-decoding matrix.
func TestReadQueryParity(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) {
		c.SearchFields = []string{"name"}
		c.Pagination = &entity.PaginationConfig{CursorField: "seq", MaxListLimit: 500}
	}, 3)
	host, _ := covItems(t, func(c *entity.EntityConfig) {
		c.AllowedFilterParams = []string{"q"}
		c.Pagination = &entity.PaginationConfig{CursorField: "seq", MaxListLimit: 500}
	}, 3)
	surfaces := []struct {
		name        string
		base        url.Values
		get, direct bool
	}{
		{name: "list"},
		{name: "cursor", base: url.Values{"cursor": {""}}},
		{name: "stream", base: url.Values{"stream": {"true"}}},
		{name: "direct_stream", direct: true},
		{name: "get", get: true},
	}
	values := map[string][2]string{
		"cursor": {"", ""}, "direction": {"forward", "backward"},
		"fields": {"name", "seq"}, "include": {"", ""},
		"limit": {"2", "3"}, "offset": {"0", "1"},
		"page": {"1", "2"}, "per_page": {"2", "3"},
		"q": {"n", "other"}, "stream": {"true", "false"},
		"trashed": {"true", "false"},
		"where":   {`{"field":"name","op":"eq","value":"n"}`, `{"field":"name","op":"eq","value":"other"}`},
	}
	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			serve := func(handler *CrudHandler, raw string) *httptest.ResponseRecorder {
				req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?"+raw, nil), "u1")
				rec := httptest.NewRecorder()
				switch {
				case surface.get:
					req.SetPathValue("id", "a")
					handler.Get()(rec, req)
				case surface.direct:
					handler.ServeStreamingList(req.Context(), rec, req, []string{"id", "seq", "name"}, nil, nil, nil, 1, 10, nil)
				default:
					handler.List()(rec, req)
				}
				return rec
			}
			for _, key := range singleValueQueryKeys {
				t.Run(key, func(t *testing.T) {
					pair, ok := values[key]
					if !ok {
						t.Fatalf("no accepted values for control key %q", key)
					}
					q := url.Values{}
					for k, v := range surface.base {
						q[k] = append([]string(nil), v...)
					}
					// Prove either value is accepted on its own before checking the
					// repeat, so another validation error cannot make the test pass.
					for _, v := range pair {
						q.Set(key, v)
						if rec := serve(ch, q.Encode()); rec.Code != http.StatusOK {
							t.Fatalf("single %s=%q: %d %s", key, v, rec.Code, rec.Body)
						}
					}
					q[key] = pair[:]
					want := "query_key_repeated"
					if key == "include" {
						want = "include_repeated"
					}
					t.Run("repeated", func(t *testing.T) {
						if rec := serve(ch, q.Encode()); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
							t.Fatalf("%s: %d %s, want 400 %s", q.Encode(), rec.Code, rec.Body, want)
						}
					})
					t.Run("undecodable", func(t *testing.T) {
						q.Del(key)
						if rec := serve(ch, q.Encode()+"&"+key+"=%ZZ"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "query_undecodable") {
							t.Fatalf("%s=%%ZZ: %d %s, want 400 query_undecodable", key, rec.Code, rec.Body)
						}
					})
				})
			}
			t.Run("host_owned_q_may_repeat", func(t *testing.T) {
				q := url.Values{"q": {"red", "blue"}}
				for k, v := range surface.base {
					q[k] = v
				}
				if rec := serve(host, q.Encode()); rec.Code != http.StatusOK {
					t.Fatalf("host-owned repeated q: %d %s", rec.Code, rec.Body)
				}
			})

			t.Run("sort_may_repeat", func(t *testing.T) {
				q := url.Values{"sort": {"name", "-seq"}}
				for k, v := range surface.base {
					q[k] = v
				}
				if rec := serve(ch, q.Encode()); rec.Code != http.StatusOK {
					t.Fatalf("repeated sort: %d %s", rec.Code, rec.Body)
				}
			})
		})
	}
}

// A hook can change the request after entry. Every downstream path must use
// the entry values, including the cursor anchor and streaming delegation.
func TestReadQueryListUsesEntryValues(t *testing.T) {
	for _, mode := range []string{"", "&cursor=", "&stream=true"} {
		t.Run(mode, func(t *testing.T) {
			ch, _ := covItems(t, func(c *entity.EntityConfig) {
				c.Pagination = &entity.PaginationConfig{CursorField: "seq", MaxListLimit: 500}
			}, 3)
			ch.Hooks = hook.NewHookRegistry()
			ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
				data.(*hook.ListPayload).Request.URL.RawQuery = "limit=1&direction=backward&offset=2&trashed=%ZZ"
				return nil
			})
			req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?limit=2"+mode, nil), "u1")
			rec := httptest.NewRecorder()
			ch.List()(rec, req)
			var response struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusOK || len(response.Data) != 2 || response.Data[0]["id"] != "a" || response.Data[1]["id"] != "b" {
				t.Fatalf("downstream query changed after entry: %d %s", rec.Code, rec.Body)
			}
		})
	}
}
