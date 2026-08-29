package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// noopPrompt is a do-nothing prompt handler for listing tests that never
// call prompts/get.
func noopPrompt(context.Context, map[string]string) ([]PromptMessage, error) {
	return nil, nil
}

// mustRegisterResource registers a resource or fails the test.
func mustRegisterResource(t *testing.T, s *Server, uri, name string) {
	t.Helper()
	if err := s.RegisterResource(uri, name, "text/plain",
		func(context.Context) (ResourceContents, error) { return ResourceContents{}, nil }); err != nil {
		t.Fatalf("register resource %s: %v", uri, err)
	}
}

// walkList pages a paginated list method to exhaustion, following
// nextCursor exactly as a spec client does. Fails on an error response
// (including a forged-cursor error mid-walk) and on a walk that exceeds
// maxPages (a nextCursor that never terminates).
func walkList(t *testing.T, s *Server, ctx context.Context, method string) []map[string]any {
	t.Helper()
	const maxPages = 64
	var pages []map[string]any
	cursor := ""
	for range maxPages {
		req := Request{JSONRPC: "2.0", ID: 1, Method: method}
		if cursor != "" {
			p, _ := json.Marshal(map[string]string{"cursor": cursor})
			req.Params = p
		}
		resp := s.HandleRequest(ctx, req)
		if resp.Error != nil {
			t.Fatalf("%s: page %d errored: %v", method, len(pages)+1, resp.Error)
		}
		m := wireResult(t, resp)
		pages = append(pages, m)
		next, _ := m["nextCursor"].(string)
		if next == "" {
			return pages
		}
		cursor = next
	}
	t.Fatalf("%s: nextCursor did not terminate within %d pages", method, maxPages)
	return nil
}

// pageFieldNames pulls one string field out of every item of a page's
// result array (e.g. tools[].name, resources[].uri).
func pageFieldNames(m map[string]any, key, field string) []string {
	items, _ := m[key].([]any)
	names := make([]string, 0, len(items))
	for _, it := range items {
		if im, ok := it.(map[string]any); ok {
			if s, ok := im[field].(string); ok {
				names = append(names, s)
			}
		}
	}
	return names
}

// Every paginated list method walks its whole set across pages: each item
// exactly once (no duplicates, no gaps), pages cut at the page size until
// the short final page, order stable and ascending across requests, and
// every non-final page carrying nextCursor while the final one omits it.
func TestListPaginationCoversAllItems(t *testing.T) {
	s := NewServer()
	s.listPageSize = 3
	const n = 10
	for i := range n {
		name := fmt.Sprintf("item_%02d", i)
		mustRegisterOpen(t, s, name)
		mustRegisterPrompt(t, s, name, noopPrompt)
		mustRegisterResource(t, s, "ui://"+name, name)
		mustRegisterTemplate(t, s, "ui://"+name+"/{id}", name)
	}
	cases := []struct {
		method string
		key    string
		field  string
		want   func(name string) string
	}{
		{"tools/list", "tools", "name", func(name string) string { return name }},
		{"resources/list", "resources", "uri", func(name string) string { return "ui://" + name }},
		{"resources/templates/list", "resourceTemplates", "uriTemplate", func(name string) string { return "ui://" + name + "/{id}" }},
		{"prompts/list", "prompts", "name", func(name string) string { return name }},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			pages := walkList(t, s, context.Background(), tc.method)
			if len(pages) != 4 { // 10 items at 3 per page
				t.Fatalf("got %d pages, want 4", len(pages))
			}
			var all []string
			for i, p := range pages {
				if i < len(pages)-1 {
					if _, ok := p["nextCursor"]; !ok {
						t.Errorf("page %d/%d omitted nextCursor mid-walk", i+1, len(pages))
					}
				}
				wantSize := 3
				if i == len(pages)-1 {
					wantSize = 1
					if _, ok := p["nextCursor"]; ok {
						t.Errorf("final page carried nextCursor: %v", p["nextCursor"])
					}
				}
				names := pageFieldNames(p, tc.key, tc.field)
				if len(names) != wantSize {
					t.Errorf("page %d/%d: got %d items, want %d", i+1, len(pages), len(names), wantSize)
				}
				all = append(all, names...)
			}
			if !slices.IsSorted(all) {
				t.Errorf("pages not in ascending order: %v", all)
			}
			slices.Sort(all)
			wantAll := make([]string, 0, n)
			for i := range n {
				wantAll = append(wantAll, tc.want(fmt.Sprintf("item_%02d", i)))
			}
			if !slices.Equal(all, wantAll) {
				t.Errorf("walked set has gaps or duplicates:\n got %v\nwant %v", all, wantAll)
			}
		})
	}
}

// A set smaller than one page answers with the exact pre-pagination wire
// shape: no nextCursor key at all. That absence is the compatibility
// contract for clients written before pagination existed.
func TestListShortSetOmitsNextCursor(t *testing.T) {
	s := NewServer()
	mustRegisterOpen(t, s, "solo")

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("tools/list errored: %v", resp.Error)
	}
	got := wireJSON(t, resp)
	want := `{"tools":[{"name":"solo","description":"d","inputSchema":{"type":"object"}}]}`
	if got != want {
		t.Errorf("tools/list short-set wire shape:\n got %s\nwant %s", got, want)
	}

	mustRegisterPrompt(t, s, "solo_p", noopPrompt)
	mustRegisterResource(t, s, "ui://solo", "solo")
	mustRegisterTemplate(t, s, "ui://solo/{id}", "solo")
	for _, method := range []string{"resources/list", "resources/templates/list", "prompts/list"} {
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: method})
		if resp.Error != nil {
			t.Fatalf("%s errored: %v", method, resp.Error)
		}
		if blob := wireJSON(t, resp); strings.Contains(blob, "nextCursor") {
			t.Errorf("%s: short set carried nextCursor: %s", method, blob)
		}
	}
}

// The cursor is client-visible, so it must be unforgeable: garbage, a
// wrong version, a tampered payload, a tampered MAC and a cursor minted
// for a different list method are all clean -32602 errors — never a
// panic, never a silent reset to page 1 (an accepted forged cursor would
// answer 200 with page 1, which the Error check catches).
func TestForgedCursorIsRejected(t *testing.T) {
	s := NewServer()
	s.listPageSize = 1
	mustRegisterOpen(t, s, "t0")
	mustRegisterOpen(t, s, "t1")
	mustRegisterPrompt(t, s, "p0", noopPrompt)
	mustRegisterPrompt(t, s, "p1", noopPrompt)

	first := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	real, _ := wireResult(t, first)["nextCursor"].(string)
	if real == "" {
		t.Fatal("page 1 did not mint a cursor at page size 1")
	}
	parts := strings.Split(real, ".")
	if len(parts) != 3 {
		t.Fatalf("cursor is not v1.payload.mac: %q", real)
	}

	// Tampered payload: the offset swapped for 99 (out of range), MAC
	// kept. Without the HMAC check this forges an out-of-range read.
	badPayload, _ := json.Marshal(cursorPayload{Method: "tools/list", Offset: 99})
	forgedPayload := parts[0] + "." + base64.RawURLEncoding.EncodeToString(badPayload) + "." + parts[2]

	// A legitimately-signed cursor for a DIFFERENT method (prompts/list).
	promptCur := func() string {
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "prompts/list"})
		cur, _ := wireResult(t, resp)["nextCursor"].(string)
		if cur == "" {
			t.Fatal("prompts/list page 1 did not mint a cursor")
		}
		return cur
	}()

	forgeries := map[string]string{
		"garbage":            "nonsense",
		"empty":              "",
		"wrong version":      "v2." + parts[1] + "." + parts[2],
		"tampered payload":   forgedPayload,
		"tampered mac":       real[:len(real)-3] + "AAA",
		"foreign method":     promptCur,
		"bad base64 payload": "v1.!!!." + parts[2],
	}
	for name, cursor := range forgeries {
		params, _ := json.Marshal(map[string]string{"cursor": cursor})
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/list", Params: params,
		})
		if name == "empty" {
			// An omitted/empty cursor is page 1, not a forgery.
			if resp.Error != nil {
				t.Errorf("empty cursor treated as invalid: %v", resp.Error)
			}
			continue
		}
		if resp.Error == nil {
			t.Errorf("%s: forged cursor was accepted (silent page reset)", name)
			continue
		}
		if resp.Error.Code != ErrInvalidParams {
			t.Errorf("%s: error code %d, want %d (invalid params)", name, resp.Error.Code, ErrInvalidParams)
		}
	}

	// A negative offset is refused even when correctly signed: our own
	// encoder can mint one, a client never can.
	if _, err := s.decodeListCursor("tools/list", s.encodeListCursor("tools/list", -1)); err == nil {
		t.Error("correctly-signed negative offset accepted")
	}
}

// A cursor at or past the end of the set (items vanished between pages,
// or a cursor minted at the exact boundary) terminates the walk: an empty
// page, no nextCursor, no error, no panic.
func TestStaleCursorPastEndEmptyPage(t *testing.T) {
	s := NewServer()
	mustRegisterOpen(t, s, "only")
	for _, offset := range []int{1, 5} {
		params, _ := json.Marshal(map[string]string{"cursor": s.encodeListCursor("tools/list", offset)})
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/list", Params: params,
		})
		if resp.Error != nil {
			t.Fatalf("stale cursor (offset %d) errored: %v", offset, resp.Error)
		}
		m := wireResult(t, resp)
		if tools := m["tools"].([]any); len(tools) != 0 {
			t.Errorf("offset %d: want empty page, got %v", offset, tools)
		}
		if _, ok := m["nextCursor"]; ok {
			t.Errorf("offset %d: empty page carried nextCursor", offset)
		}
	}
}
