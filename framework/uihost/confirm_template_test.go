package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"

	// The kit registers the confirm dialog; this binary links it the
	// way every real host does.
	_ "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Every page carries the kit's confirm dialog once, in the body, and
// its head links the dialog's sheet: a
// data-cui-confirm control on any page, or on a page reached later by
// client navigation, asks in the kit's dialog and not window.confirm.
func TestPageCarriesConfirmDialog(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()

	const open = `<template data-cui-confirm-dialog=""><dialog`
	if n := strings.Count(body, open); n != 1 {
		t.Fatalf("page carries %d confirm dialog template(s), want 1:\n%s", n, body)
	}
	at := strings.Index(body, open)
	if at < strings.Index(body, "<body") || at > strings.LastIndex(body, "</body>") {
		t.Fatalf("the confirm template is not inside the body:\n%s", body)
	}
	tail := body[at:]
	end := strings.Index(tail, "</template>")
	if end < 0 {
		t.Fatalf("the confirm template never closes:\n%s", tail)
	}
	for _, part := range []string{"title", "message", "cancel", "accept", "accept-danger"} {
		if !strings.Contains(tail[:end], `data-cui-confirm-part="`+part+`"`) {
			t.Errorf("the confirm dialog has no %s part:\n%s", part, tail[:end])
		}
	}
	head := body[:strings.Index(body, "</head>")]
	if !strings.Contains(head, "ui-confirm-dialog") {
		t.Errorf("the head does not link the confirm dialog's sheet:\n%s", head)
	}
}
