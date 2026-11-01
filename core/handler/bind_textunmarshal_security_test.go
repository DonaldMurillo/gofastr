package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Pins: a tagged field whose type implements encoding.TextUnmarshaler binds
// through UnmarshalText on every tag surface (query, path, header, and the
// ToolAdapter's tagged params), the same parser a JSON body runs.
// Finding (2026-11 hunt, H7): `type Status string` with an UnmarshalText
// that allows only open/closed was refused in a JSON body, while
// ?status=bogus bound "bogus" raw through setField's string case. The
// validator the type carries was skipped on exactly the transport a
// browser link can reach.

// ticketStatus is an enum that validates its own text.
type ticketStatus string

func (s *ticketStatus) UnmarshalText(b []byte) error {
	switch v := string(b); v {
	case "open", "closed":
		*s = ticketStatus(v)
		return nil
	default:
		return fmt.Errorf("status %q is not open or closed", v)
	}
}

type ticketFilter struct {
	Status   ticketStatus  `query:"status" json:"-"`
	StatusP  *ticketStatus `query:"maybe" json:"-"`
	Since    time.Time     `query:"since" json:"-"`
	Until    *time.Time    `query:"until" json:"-"`
	Header   ticketStatus  `header:"X-Status" json:"-"`
	PathStat ticketStatus  `path:"st" json:"-"`
}

func bindStatus(t *testing.T, target string) (ticketFilter, error) {
	t.Helper()
	var in ticketFilter
	err := Bind(httptest.NewRequest(http.MethodGet, target, nil), &in)
	return in, err
}

func requireInvalidParam(t *testing.T, err error, name string) {
	t.Helper()
	var he *Error
	if !errors.As(err, &he) {
		t.Fatalf("want *Error, got %v", err)
	}
	if he.Code != 400 || he.Reason.Code() != ReasonInvalidParameter.Code() {
		t.Fatalf("want 400 %s, got %d %s", ReasonInvalidParameter.Code(), he.Code, he.Reason.Code())
	}
	if got, _ := he.Params["name"].(string); got != `"`+name+`"` {
		t.Fatalf("refusal names %q, want %q", got, name)
	}
}

func TestBindQueryRunsTheTypesTextValidator(t *testing.T) {
	_, err := bindStatus(t, "/tickets?status=bogus")
	if err == nil {
		t.Fatal("SECURITY: [bind-textunmarshal] ?status=bogus bound past the type's own " +
			"UnmarshalText validator, which a JSON body would have run")
	}
	requireInvalidParam(t, err, "status")

	in, err := bindStatus(t, "/tickets?status=closed")
	if err != nil || in.Status != "closed" {
		t.Fatalf("a valid member did not bind: %+v %v", in, err)
	}
}

func TestBindPointerTextUnmarshaler(t *testing.T) {
	in, err := bindStatus(t, "/tickets?maybe=open")
	if err != nil || in.StatusP == nil || *in.StatusP != "open" {
		t.Fatalf("*ticketStatus did not bind: %+v %v", in, err)
	}
	if _, err := bindStatus(t, "/tickets?maybe=nope"); err == nil {
		t.Fatal("SECURITY: [bind-textunmarshal] a pointer field skipped the validator")
	} else {
		requireInvalidParam(t, err, "maybe")
	}
	in, err = bindStatus(t, "/tickets")
	if err != nil || in.StatusP != nil {
		t.Fatalf("absent pointer param allocated: %+v %v", in, err)
	}
}

func TestBindTimeQueryParam(t *testing.T) {
	in, err := bindStatus(t, "/tickets?since=2026-11-01T09:30:00Z&until=2026-11-02T00:00:00%2B02:00")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC); !in.Since.Equal(want) {
		t.Fatalf("since = %v, want %v", in.Since, want)
	}
	if in.Until == nil || !in.Until.Equal(time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("until = %v", in.Until)
	}
	if _, err := bindStatus(t, "/tickets?since=yesterday"); err == nil {
		t.Fatal("an unparsable time bound")
	} else {
		requireInvalidParam(t, err, "since")
	}
}

func TestBindHeaderAndPathRunTheTextValidator(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	req.Header.Set("X-Status", "bogus")
	var in ticketFilter
	err := Bind(req, &in)
	var he *Error
	if !errors.As(err, &he) || he.Code != 400 || he.Reason.Code() != ReasonInvalidHeader.Code() {
		t.Fatalf("SECURITY: [bind-textunmarshal] header skipped the validator: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/tickets/bogus", nil)
	req.SetPathValue("st", "bogus")
	in = ticketFilter{}
	if err := Bind(req, &in); err == nil {
		t.Fatal("SECURITY: [bind-textunmarshal] path value skipped the validator")
	}
}

func TestToolAdapterRunsTheTextValidator(t *testing.T) {
	echo := Handler[ticketFilter, ticketFilter](func(_ context.Context, in ticketFilter) (ticketFilter, error) { return in, nil })
	_, err := ToolAdapter(echo)(context.Background(), map[string]any{"status": "bogus"})
	if err == nil {
		t.Fatal("SECURITY: [bind-textunmarshal] tool param status=bogus bound past UnmarshalText")
	}
	requireInvalidParam(t, err, "status")

	out, err := ToolAdapter(echo)(context.Background(), map[string]any{"status": "open", "since": "2026-11-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	got := out.(ticketFilter)
	if got.Status != "open" || got.Since.IsZero() {
		t.Fatalf("got %+v", got)
	}
	if got.StatusP != nil {
		t.Fatalf("absent pointer param allocated: %+v", got)
	}
}
