package handler

import (
	"context"
	"strings"
	"testing"
)

type refundInput struct {
	ID     string `path:"id" json:"-"`
	Notify bool   `query:"notify" json:"-"`
	Amount string `json:"amount"`
}

func echoRefund(_ context.Context, in refundInput) (refundInput, error) { return in, nil }

func TestToolAdapterBindsLikeTheRoute(t *testing.T) {
	out, err := ToolAdapter(Handler[refundInput, refundInput](echoRefund))(context.Background(), map[string]any{
		"id": "42", "notify": true, "amount": "9.50",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(refundInput); got != (refundInput{ID: "42", Notify: true, Amount: "9.50"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestToolAdapterRefusesUnknownKey(t *testing.T) {
	_, err := ToolAdapter(Handler[refundInput, refundInput](echoRefund))(context.Background(), map[string]any{
		"id": "42", "amont": "9.50",
	})
	if err == nil {
		t.Fatal("a misspelled param was dropped")
	}
}

func TestToolAdapterRefusesPathEscape(t *testing.T) {
	for _, id := range []string{"42/../7", "..", "4\n2", "a/b"} {
		_, err := ToolAdapter(Handler[refundInput, refundInput](echoRefund))(context.Background(), map[string]any{"id": id})
		if err == nil {
			t.Errorf("path param %q accepted", id)
		}
	}
}

func TestToolAdapterPanicIsGeneric(t *testing.T) {
	boom := Handler[refundInput, refundInput](func(context.Context, refundInput) (refundInput, error) {
		panic("db password=hunter2")
	})
	_, err := ToolAdapter(boom)(context.Background(), map[string]any{"id": "1"})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("panic leaked or vanished: %v", err)
	}
}

func TestToolAdapterNumberPathParam(t *testing.T) {
	out, err := ToolAdapter(Handler[refundInput, refundInput](echoRefund))(context.Background(), map[string]any{"id": float64(42)})
	if err != nil || out.(refundInput).ID != "42" {
		t.Fatalf("got %v %v", out, err)
	}
}
