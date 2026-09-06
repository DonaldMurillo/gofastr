package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestMCPNumericFilterMatchesStoredInt drives the real JSON-RPC tools/call
// wire path (not a pre-decoded float64 map) so an integer filter above 2^53
// addresses its row. The transport decodes arguments with UseNumber
// (core/mcp/tools.go), so 9007199254740993 stays a json.Number and
// toolParamString renders it verbatim into the re-dispatched query, instead
// of the "9.007199254740992e+15" a plain float64 decode produced. Supersedes
// the round-4 red probe mcp_number_red_test.go, which mirrored the OLD decode
// in-process and so could not observe the transport fix.
func TestMCPNumericFilterMatchesStoredInt(t *testing.T) {
	installSecurityOwnerExtractor(t)

	db := setupDB(t, `CREATE TABLE mcp_ledger (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, amount INTEGER)`)
	ent := entity.Define("mcp_ledger", makeEntityConfig("mcp_ledger", "mcp_ledger", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "amount", Type: schema.Int},
	}))
	ent.SetDB(db)
	seedRows(t, db, "mcp_ledger", []map[string]any{
		{"id": "big", "user_id": "alice", "amount": 9007199254740993},
		{"id": "small", "user_id": "alice", "amount": 7},
	})

	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/mcp_ledger")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}

	orig, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	ctx := mcp.WithRequest(context.Background(), orig)
	ctx = handler.SetUser(ctx, &testUser{id: "alice"})

	// call sends a real tools/call whose `arguments` blob is spliced in
	// verbatim (json.RawMessage), so a large integer keeps every digit on
	// the wire the way an MCP client sends it.
	call := func(rawArgs string) string {
		p, err := json.Marshal(map[string]any{
			"name":      "mcp_ledger_list",
			"arguments": json.RawMessage(rawArgs),
		})
		if err != nil {
			t.Fatal(err)
		}
		resp := srv.HandleRequest(ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p})
		if resp.Error != nil {
			t.Fatalf("tools/call %s: %+v", rawArgs, resp.Error)
		}
		out, err := json.Marshal(resp.Result)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}

	// Control: a small integer addresses only its row.
	small := call(`{"amount": 7}`)
	if !strings.Contains(small, `\"id\":\"small\"`) || strings.Contains(small, `\"id\":\"big\"`) {
		t.Fatalf("control: amount=7 must match only the small row, got: %s", small)
	}

	// The finding: the big integer matches the row that stores it exactly.
	big := call(`{"amount": 9007199254740993}`)
	if !strings.Contains(big, `\"id\":\"big\"`) {
		t.Errorf("SECURITY: [mcp-number] tools/call list with amount=9007199254740993 did not match the row storing exactly that value — the argument lost precision on the wire; UseNumber must carry the exact decimal into the filter. got: %s", big)
	}
}
