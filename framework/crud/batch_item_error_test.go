package crud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A batch item reports the refusal the single write sends for the same
// error. classifyDoErr knew validation, hook, not-found and no-fields
// errors only, so a state the write may not set, a unique or foreign-key
// conflict and the transition refusals came back as a 500 "internal
// server error" in a batch while the single route said what was wrong
// (2026-10-27 bug bash D37).
func TestBatchItemErrorMatchesSingleWrite(t *testing.T) {
	ctx := context.Background()
	notNull := &constraintFieldsError{
		err:    errors.New("NOT NULL constraint failed: bn.title"),
		fields: map[string][]schema.Violation{"title": {{Reason: schema.ReasonRequired}}},
	}
	for _, err := range []error{
		&ValidationError{fields: map[string][]schema.Violation{"a": {{Reason: schema.ReasonRequired}}}},
		&beforeHookError{err: errors.New("hook said no")},
		errNotFound,
		errNoFieldsToUpdate,
		&StateError{Field: "status", Current: "draft", Moves: []string{"publish"}},
		&StateError{Field: "status", Current: "draft", hidden: true},
		&TransitionConflictError{Key: "publish", Current: "draft", Moves: []string{"archive"}},
		&transitionDeniedError{perm: "posts:publish"},
		ErrUnknownTransition,
		ErrReentrantMove,
		&tenantMissingError{},
		fmt.Errorf("insert: %w", errors.New("UNIQUE constraint failed: bn.title")),
		errors.New("FOREIGN KEY constraint failed"),
		notNull,
		errors.New("driver boom"),
	} {
		rec := httptest.NewRecorder()
		writeCRUDError(rec, httptest.NewRequest(http.MethodPost, "/bn", nil), err)
		var body struct {
			Error        string              `json:"error"`
			Reason       string              `json:"reason"`
			Fields       map[string][]string `json:"fields"`
			FieldReasons map[string][]string `json:"field_reasons"`
		}
		if jerr := json.Unmarshal(rec.Body.Bytes(), &body); jerr != nil {
			t.Fatalf("%v: decode: %v", err, jerr)
		}
		// A refusal with no code (an app hook's own wording) sends no
		// "reason" key at all, as the batch item's omitempty does.
		var keys map[string]json.RawMessage
		if jerr := json.Unmarshal(rec.Body.Bytes(), &keys); jerr != nil {
			t.Fatalf("%v: decode: %v", err, jerr)
		}
		_, hasReason := keys["reason"]
		res := classifyDoErr(ctx, 2, err)
		if hasReason != (res.Reason != "") {
			t.Errorf("%T %v: single write sends reason %v, batch item reason %q", err, err, hasReason, res.Reason)
		}
		if res.Index != 2 || res.Error != body.Error || res.Reason != body.Reason ||
			!maps.EqualFunc(res.Fields, body.Fields, slices.Equal) ||
			!maps.EqualFunc(res.FieldReasons, body.FieldReasons, slices.Equal) {
			t.Errorf("%T %v: batch item %+v, single write %d %+v", err, err, res, rec.Code, body)
		}
	}
}
