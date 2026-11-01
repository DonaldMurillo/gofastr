package crud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// MaxBatchSize caps how many items a single _batch request may contain.
// The cap exists to bound transaction duration and protect against
// pathological payloads.
const MaxBatchSize = 100

// decodeBatchEnvelope decodes a _batch request body into v, but only after
// checking that the top-level object carries exactly the keys the envelope
// declares — each at most once, spelled exactly.
//
// encoding/json alone accepts all three smuggling shapes. A duplicated key is
// last-one-wins, so a validator or proxy that inspects the FIRST "items"
// array sees a different payload than the one this handler executes. Field
// matching is case-insensitive, so "Items" binds to Items and slips past any
// upstream check keyed on the exact name. And an unrecognised key is silently
// dropped rather than refused. The envelope is a fixed, one-field shape; there
// is nothing to gain by being lenient about it.
func decodeBatchEnvelope(r *http.Request, v any, allowed ...string) error {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return errBodyTooLarge
		}
		if strings.Contains(err.Error(), "request body too large") {
			return errBodyTooLarge
		}
		return bodyReadError(err)
	}
	if err := checkEnvelopeKeys(raw, allowed); err != nil {
		return err
	}
	// handler.UnmarshalStrict keeps the same refusal posture on the decode
	// itself (exact-tag match, duplicates and unknown keys refused), so
	// the vetted body needs no analyzer marker. UseNumber keeps every item
	// number as its text: a float64 decode rounded an integer past 2^53 and
	// refused a JSON field's 1e400, which a single write stores.
	if err := handler.UnmarshalStrict(raw, v, handler.UseNumber()); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// checkEnvelopeKeys walks the top-level object's keys without decoding its
// values, so a hostile payload is rejected before anything binds.
func checkEnvelopeKeys(raw []byte, allowed []string) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return bodyDecodeError(err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return envelopeError(reasonEnvelopeNotObject, nil)
	}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return bodyDecodeError(err)
		}
		key, ok := kt.(string)
		if !ok {
			return envelopeError(reasonEnvelopeBadKey, nil)
		}
		if seen[key] {
			return envelopeError(reasonEnvelopeDuplicateKey, map[string]any{"key": strconv.Quote(key)})
		}
		seen[key] = true
		if !slices.Contains(allowed, key) {
			quoted := make([]string, len(allowed))
			for i, a := range allowed {
				quoted[i] = strconv.Quote(a)
			}
			return envelopeError(reasonEnvelopeUnknownKey, map[string]any{"key": strconv.Quote(key), "expected": strings.Join(quoted, ", ")})
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return bodyDecodeError(err)
		}
	}
	return nil
}

// envelopeError is checkEnvelopeKeys' refusal: invalid_body, its detail
// the phrase naming what is wrong with the envelope, so the message
// translates and the reason code a client branches on stays invalid_body.
func envelopeError(detail i18n.Phrase, params map[string]any) error {
	return &i18n.Error{Reason: reasonInvalidBody, Params: map[string]any{"detail": i18n.NewError(detail, params)}}
}

// errBatchAborted is the sentinel returned from inside inTx when a batch
// operation hit a per-item error and needs the whole tx to roll back. The
// real failure detail lives on the per-item result.
var errBatchAborted = errors.New("batch aborted")

// batchResult is one entry in a _batch response. Exactly one of Data, Error,
// or Skipped will be populated for any given index. When the tx rolled back
// because a later item failed, earlier successes are still recorded with
// their constructed Data, but the top-level Committed=false signals that
// nothing was persisted.
type batchResult struct {
	Index        int                 `json:"index"`
	Data         map[string]any      `json:"data,omitempty"`
	Error        string              `json:"error,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	Fields       map[string][]string `json:"fields,omitempty"`
	FieldReasons map[string][]string `json:"field_reasons,omitempty"`
	Skipped      bool                `json:"skipped,omitempty"`
}

// BatchResponse is the standard envelope for all _batch endpoints.
type BatchResponse struct {
	Committed bool          `json:"committed"`
	Results   []batchResult `json:"results"`
}

// classifyDoErr reports one item's error as the single write refuses
// it (crudRefusal): the same message in ctx's locale, reason code and
// field errors. Validation, hook, state, transition and constraint
// refusals are the caller's to act on and carry no driver text; anything
// else is the generic "internal server error", with the error itself
// only logged, so the per-item Error field cannot leak schema or
// connection details.
func classifyDoErr(ctx context.Context, index int, err error) batchResult {
	f := crudRefusal(ctx, err)
	return batchResult{Index: index, Error: f.text, Reason: f.reason, Fields: f.fields, FieldReasons: f.fieldReasons}
}

// initSkipped pre-fills a results slice with skipped entries. Each entry is
// later replaced with data or error as the loop progresses; anything left
// skipped means the batch aborted before reaching that index.
func initSkipped(n int) []batchResult {
	out := make([]batchResult, n)
	for i := range out {
		out[i] = batchResult{Index: i, Skipped: true}
	}
	return out
}

// writeBatchResponse marshals the envelope and selects 200 vs 400 based on
// whether the tx committed. When the batch was rolled back, per-item
// success Data is scrubbed: a caller that reads it without checking
// Committed=false would otherwise treat the constructed-but-not-
// persisted shape as authoritative.
func writeBatchResponse(w http.ResponseWriter, resp BatchResponse) {
	w.Header().Set("Content-Type", "application/json")
	if !resp.Committed {
		scrubRolledBackData(resp.Results)
		w.WriteHeader(http.StatusBadRequest)
	}
	json.NewEncoder(w).Encode(resp)
}

// scrubRolledBackData clears the Data field on per-item results whose
// only error is "the surrounding tx rolled back". The contract: when
// Committed=false, no per-item Data appears in the response. Errors and
// Fields stay, they're the actionable parts.
func scrubRolledBackData(results []batchResult) {
	for i := range results {
		results[i].Data = nil
	}
}

// ============================================================================
// Batch Create
// ============================================================================

type batchCreateRequest struct {
	Items []map[string]any `json:"items"`
}

// BatchCreate returns an http.HandlerFunc accepting POST /{table}/_batch with
// {items:[...]}. All items run in one transaction; the first per-item error
// rolls everything back. The response always includes a results array in
// input order.
func (ch *CrudHandler) BatchCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Per-caller rows, as on the single-row routes: never stored by a
		// shared cache, on every arm (Delete in crud.go says why).
		w.Header().Set("Cache-Control", "no-store")
		if err := enforceJSONOnly(r); err != nil {
			writeContentTypeError(w, r, err)
			return
		}
		if !ch.requireScope(w, r, opCreate) {
			return
		}
		limitJSONBody(w, r)
		var req batchCreateRequest
		if err := decodeBatchEnvelope(r, &req, "items"); err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidBody)
			return
		}
		normalizeItemNumbers(req.Items)
		if len(req.Items) == 0 {
			writeJSONError(w, r, http.StatusBadRequest, reasonItemsEmpty)
			return
		}
		if len(req.Items) > MaxBatchSize {
			writeJSONError(w, r, http.StatusBadRequest, reasonBatchTooLarge, map[string]any{"size": len(req.Items), "max": MaxBatchSize})
			return
		}
		// The same fold refusal the single-record path applies pre-decode:
		// two distinct item keys folding onto one column must be rejected
		// deterministically, not resolved by map iteration order in
		// unconvertMapKeys.
		for i, item := range req.Items {
			if err := ch.itemKeyFoldsUnambiguous(item); err != nil {
				writeBatchItemError(w, r, i, err)
				return
			}
		}

		results := initSkipped(len(req.Items))
		// hidden marks items whose created row the caller's ReadScope hides;
		// they answer with the id only, as the single Create route does.
		hidden := make([]bool, len(req.Items))
		txErr := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			for i, item := range req.Items {
				body := ch.unconvertMapKeys(item)
				res, err := ch.doCreate(ctx, r, body)
				if err == nil {
					hidden[i], err = ch.readScopeHidesRow(ctx, res)
				}
				if err != nil {
					results[i] = classifyDoErr(ctx, i, err)
					return errBatchAborted
				}
				results[i] = batchResult{Index: i, Data: res}
			}
			return nil
		})

		if txErr != nil && !errors.Is(txErr, errBatchAborted) {
			logf("crud: batch tx failed: %v", txErr)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		if txErr == nil {
			for _, res := range results {
				if res.Data != nil {
					ch.EmitEvent(r.Context(), event.EntityCreated, res.Data)
				}
			}
		}

		// AfterGet over each item body, after the transaction has committed
		// and the RAW record has gone to the bus, exactly as the single-record
		// routes do it. Running it inside the closure (as the first version of
		// this did) put a read hook in the write transaction: a hook that
		// touches the DB deadlocks against a single-connection pool, a hook
		// error rolls the whole batch back, and the bus received the redacted
		// clone instead of the stored row.
		// Skipped entirely when the transaction rolled back: writeBatchResponse
		// scrubs every item's Data in that case, so hooking it is wasted work
		// whose error path replaced a per-item failure report with an opaque
		// 500.
		if txErr == nil {
			for i, res := range results {
				if res.Data == nil {
					continue
				}
				if hidden[i] || ch.getScopeHidesRow(r, res.Data) {
					results[i].Data = ch.identityOnly(res.Data)
					continue
				}
				body, hookErr := ch.runResponseHooks(r, res.Data)
				if hookErr != nil {
					// Degrade this item to its id, the batch is committed, and
					// a 500 would lose the ids of rows already in the table.
					logf("crud: after-get hook failed on batch response, returning id only: %v", hookErr)
					body = ch.identityOnly(res.Data)
				}
				results[i].Data = body
			}
		}
		writeBatchResponse(w, BatchResponse{Committed: txErr == nil, Results: results})
	}
}

// ============================================================================
// Batch Update
// ============================================================================

type batchUpdateRequest struct {
	Items []map[string]any `json:"items"`
}

// BatchUpdate returns an http.HandlerFunc accepting PATCH /{table}/_batch.
// Each item must contain an "id" field naming the record to patch; remaining
// fields are the partial update. All items share one transaction.
func (ch *CrudHandler) BatchUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
		// Per-caller rows, as on the single-row routes: never stored by a
		// shared cache, on every arm (Delete in crud.go says why).
		w.Header().Set("Cache-Control", "no-store")
		if err := enforceJSONOnly(r); err != nil {
			writeContentTypeError(w, r, err)
			return
		}
		if !ch.requireScope(w, r, opUpdate) {
			return
		}
		limitJSONBody(w, r)
		var req batchUpdateRequest
		if err := decodeBatchEnvelope(r, &req, "items"); err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidBody)
			return
		}
		normalizeItemNumbers(req.Items)
		if len(req.Items) == 0 {
			writeJSONError(w, r, http.StatusBadRequest, reasonItemsEmpty)
			return
		}
		if len(req.Items) > MaxBatchSize {
			writeJSONError(w, r, http.StatusBadRequest, reasonBatchTooLarge, map[string]any{"size": len(req.Items), "max": MaxBatchSize})
			return
		}
		// Fold refusal, same as BatchCreate: two distinct item keys folding
		// onto one column are rejected before the transaction opens.
		for i, item := range req.Items {
			if err := ch.itemKeyFoldsUnambiguous(item); err != nil {
				writeBatchItemError(w, r, i, err)
				return
			}
		}

		results := initSkipped(len(req.Items))
		// hidden marks items whose written row the caller's ReadScope hides;
		// they answer with the id only, as the single Update route does.
		hidden := make([]bool, len(req.Items))
		txErr := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			for i, item := range req.Items {
				body := ch.unconvertMapKeys(item)
				id, ok := entity.RowKey(body, ch.keyCols(), nil)
				if !ok {
					// The item names its record under the key's wire names.
					keys := make([]string, len(ch.keyCols()))
					for k, c := range ch.keyCols() {
						keys[k] = ch.convertKey(c)
					}
					f := phraseRefusal(ctx, http.StatusBadRequest, reasonItemMissingKey, map[string]any{"key": strings.Join(keys, ", ")})
					results[i] = batchResult{Index: i, Error: f.text, Reason: f.reason}
					return errBatchAborted
				}
				for _, c := range ch.keyCols() {
					delete(body, c)
				}
				if !ch.itemPermitted(ctx, opUpdate, id) {
					results[i] = batchResult{Index: i, Error: reasonAccessDenied.Text(ctx), Reason: reasonAccessDenied.Code()}
					return errBatchAborted
				}
				res, err := ch.doUpdate(ctx, r, id, body)
				if err == nil {
					hidden[i], err = ch.readScopeHidesRow(ctx, res)
				}
				if err != nil {
					results[i] = classifyDoErr(ctx, i, err)
					return errBatchAborted
				}
				results[i] = batchResult{Index: i, Data: res}
			}
			return nil
		})

		if txErr != nil && !errors.Is(txErr, errBatchAborted) {
			logf("crud: batch tx failed: %v", txErr)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		if txErr == nil {
			for _, res := range results {
				if res.Data != nil {
					ch.EmitEvent(r.Context(), event.EntityUpdated, res.Data)
				}
			}
		}

		// AfterGet over each item body, after the transaction has committed
		// and the RAW record has gone to the bus, exactly as the single-record
		// routes do it. Running it inside the closure (as the first version of
		// this did) put a read hook in the write transaction: a hook that
		// touches the DB deadlocks against a single-connection pool, a hook
		// error rolls the whole batch back, and the bus received the redacted
		// clone instead of the stored row.
		// Skipped entirely when the transaction rolled back: writeBatchResponse
		// scrubs every item's Data in that case, so hooking it is wasted work
		// whose error path replaced a per-item failure report with an opaque
		// 500.
		if txErr == nil {
			for i, res := range results {
				if res.Data == nil {
					continue
				}
				if hidden[i] || ch.getScopeHidesRow(r, res.Data) {
					results[i].Data = ch.identityOnly(res.Data)
					continue
				}
				body, hookErr := ch.runResponseHooks(r, res.Data)
				if hookErr != nil {
					// Degrade this item to its id, the batch is committed, and
					// a 500 would lose the ids of rows already in the table.
					logf("crud: after-get hook failed on batch response, returning id only: %v", hookErr)
					body = ch.identityOnly(res.Data)
				}
				results[i].Data = body
			}
		}
		writeBatchResponse(w, BatchResponse{Committed: txErr == nil, Results: results})
	}
}

// ============================================================================
// Batch Delete
// ============================================================================

type batchDeleteRequest struct {
	IDs []string `json:"ids"`
}

// BatchDelete returns an http.HandlerFunc accepting DELETE /{table}/_batch
// with {ids:[...]}. All deletes share one transaction.
func (ch *CrudHandler) BatchDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
		// Per-caller rows, as on the single-row routes: never stored by a
		// shared cache, on every arm (Delete in crud.go says why).
		w.Header().Set("Cache-Control", "no-store")
		if err := enforceJSONOnly(r); err != nil {
			writeContentTypeError(w, r, err)
			return
		}
		if !ch.requireScope(w, r, opDelete) {
			return
		}
		limitJSONBody(w, r)
		var req batchDeleteRequest
		if err := decodeBatchEnvelope(r, &req, "ids"); err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidBody)
			return
		}
		if len(req.IDs) == 0 {
			writeJSONError(w, r, http.StatusBadRequest, reasonIDsEmpty)
			return
		}
		if len(req.IDs) > MaxBatchSize {
			writeJSONError(w, r, http.StatusBadRequest, reasonBatchTooLarge, map[string]any{"size": len(req.IDs), "max": MaxBatchSize})
			return
		}

		results := initSkipped(len(req.IDs))
		txErr := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			for i, id := range req.IDs {
				if !ch.itemPermitted(ctx, opDelete, id) {
					results[i] = batchResult{Index: i, Error: reasonAccessDenied.Text(ctx), Reason: reasonAccessDenied.Code()}
					return errBatchAborted
				}
				if err := ch.doDelete(ctx, r, id); err != nil {
					results[i] = classifyDoErr(ctx, i, err)
					return errBatchAborted
				}
				results[i] = batchResult{Index: i, Data: ch.keyPayload(id)}
			}
			return nil
		})

		if txErr != nil && !errors.Is(txErr, errBatchAborted) {
			logf("crud: batch tx failed: %v", txErr)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		if txErr == nil {
			for _, res := range results {
				if res.Data != nil {
					ch.EmitEvent(r.Context(), event.EntityDeleted, res.Data)
				}
			}
		}
		writeBatchResponse(w, BatchResponse{Committed: txErr == nil, Results: results})
	}
}
