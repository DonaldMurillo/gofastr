package sdk

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// statesConfig is the invoices fixture; vary mutates one piece of it.
func statesConfig(mutate func(*entity.StatesConfig)) entity.EntityConfig {
	st := &entity.StatesConfig{
		Field:   "status",
		Initial: []string{"draft", "open"},
		Transitions: []entity.Transition{
			{Key: "issue", From: []string{"draft"}, To: "open"},
			{Key: "pay", From: []string{"open"}, To: "paid", Stamp: "paid_on"},
		},
	}
	if mutate != nil {
		mutate(st)
	}
	return entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "number", Type: schema.String, Required: true},
			{Name: "amount", Type: schema.Decimal},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "open", "paid"}, Default: "draft"},
			{Name: "paid_on", Type: schema.Date},
		},
		States: st,
	}
}

// States changes what writes a client may send (the guarded columns
// leave the write bodies, each move becomes a call), so any change to
// the block — the moves, the initial set, the Advisory release — has to
// move the hash or the drift banner reports "in sync" for a stale SDK.
func TestStatesChangeMovesSchemaHash(t *testing.T) {
	base := SchemaHash([]NamedConfig{{Name: "invoices", Config: statesConfig(nil)}})

	mutations := map[string]func(*entity.StatesConfig){
		"a new move": func(st *entity.StatesConfig) {
			st.Transitions = append(st.Transitions, entity.Transition{Key: "void", From: []string{"draft"}, To: "void"})
		},
		"a changed destination":       func(st *entity.StatesConfig) { st.Transitions[0].To = "void" },
		"a dropped stamp":             func(st *entity.StatesConfig) { st.Transitions[1].Stamp = "" },
		"a narrowed initial set":      func(st *entity.StatesConfig) { st.Initial = []string{"draft"} },
		"a move needing a permission": func(st *entity.StatesConfig) { st.Transitions[1].Permission = "invoices:pay" },
		"the advisory release":        func(st *entity.StatesConfig) { st.Advisory = true },
	}
	for name, mutate := range mutations {
		if got := SchemaHash([]NamedConfig{{Name: "invoices", Config: statesConfig(mutate)}}); got == base {
			t.Errorf("%s did not move the schema hash; an SDK generated before it would look in sync", name)
		}
	}

	// Dropping the block entirely is drift too.
	noStates := statesConfig(nil)
	noStates.States = nil
	if got := SchemaHash([]NamedConfig{{Name: "invoices", Config: noStates}}); got == base {
		t.Error("removing States did not move the schema hash")
	}
}

// The states block sitting beside a Display block does not drag screen
// hints into the hash: a Display change on a states entity stays a
// non-change, exactly as on a plain one.
func TestStatesEntityDisplayStillIgnored(t *testing.T) {
	base := SchemaHash([]NamedConfig{{Name: "invoices", Config: statesConfig(nil)}})
	withDisplay := statesConfig(nil)
	withDisplay.Display = displayRich()
	if got := SchemaHash([]NamedConfig{{Name: "invoices", Config: withDisplay}}); got != base {
		t.Fatalf("adding Display to a states entity moved the hash:\n%s\n%s", base, got)
	}
}
