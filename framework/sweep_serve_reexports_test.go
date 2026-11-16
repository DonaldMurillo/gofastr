package framework

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// The root spellings answer what their subpackage functions answer.
func TestSweepServeReexportsDelegate(t *testing.T) {
	if !IsNotFound(fmt.Errorf("get: %w", sql.ErrNoRows)) {
		t.Error("IsNotFound(wrapped sql.ErrNoRows) = false, want true")
	}
	if IsNotFound(errors.New("other")) {
		t.Error("IsNotFound(other error) = true, want false")
	}
	c := NewStringColumn("title").Eq("x")
	if reflect.DeepEqual(Not(c), c) {
		t.Error("Not returned its condition unchanged")
	}
}

// A GET that turns into an event stream after the drain started is ended
// at once instead of joining a set nobody will end again.
func TestSweepServeStreamStartedDuringDrainEnds(t *testing.T) {
	set := &openStreams{}
	set.endAll(t.Context())
	cancelled := false
	l := &longLived{cancel: func() { cancelled = true }, set: set}
	w := &streamWriter{ResponseWriter: httptest.NewRecorder(), life: l}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if !cancelled {
		t.Fatal("an event stream started during the drain was not ended")
	}
}
