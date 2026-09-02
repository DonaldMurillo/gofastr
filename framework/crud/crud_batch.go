package crud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"

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
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := checkEnvelopeKeys(raw, allowed); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil { //gofastr:allow(GOFASTR1407) checkEnvelopeKeys above already refused duplicate and case-folded top-level keys; this Unmarshal decodes a vetted body
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
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("invalid JSON: request body must be a JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		key, ok := kt.(string)
		if !ok {
			return fmt.Errorf("invalid JSON: malformed object key")
		}
		if seen[key] {
			return fmt.Errorf("duplicate key %q in request body", key)
		}
		seen[key] = true
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("unknown key %q in request body (expected %s)",
				key, strings.Join(allowed, ", "))
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
	}
	return nil
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
	Index   int                 `json:"index"`
	Data    map[string]any      `json:"data,omitempty"`
	Error   string              `json:"error,omitempty"`
	Fields  map[string][]string `json:"fields,omitempty"`
	Skipped bool                `json:"skipped,omitempty"`
}

// BatchResponse is the standard envelope for all _batch endpoints.
type BatchResponse struct {
	Committed bool          `json:"committed"`
	Results   []batchResult `json:"results"`
}

// classifyDoErr converts a per-item error into the batchResult fields it
// represents (Error string + optional Fields map).
//
// Validation and BeforeHook errors are surfaced verbatim, they're
// user-actionable and don't carry driver text. Anything else (driver
// errors, scan failures, after-hook exceptions) is redacted to a
// generic "internal error" string so the per-item Error field can't
// leak schema / connection details. The original error is still
// logged via the caller's tx logger.
func classifyDoErr(err error) (string, map[string][]string) {
	if ve, ok := errors.AsType[*ValidationError](err); ok {
		return "validation failed", ve.Fields()
	}
	if bhe, ok := errors.AsType[*beforeHookError](err); ok {
		return bhe.Error(), nil
	}
	if errors.Is(err, errNotFound) {
		return "not found", nil
	}
	if errors.Is(err, errNoFieldsToUpdate) {
		return "no fields to update", nil
	}
	log.Printf("crud: batch item failed: %v", err)
	return "internal error", nil
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
		if err := enforceJSONContentType(r); err != nil {
			writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		if !ch.requireScope(w, r, opCreate) {
			return
		}
		limitJSONBody(w, r)
		var req batchCreateRequest
		if err := decodeBatchEnvelope(r, &req, "items"); err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(req.Items) == 0 {
			writeJSONError(w, http.StatusBadRequest, "items must be non-empty")
			return
		}
		if len(req.Items) > MaxBatchSize {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("batch size %d exceeds max %d", len(req.Items), MaxBatchSize))
			return
		}

		results := initSkipped(len(req.Items))
		txErr := ch.inTx(r.Context(), func(ctx context.Context, ch *CrudHandler) error {
			for i, item := range req.Items {
				body := ch.unconvertMapKeys(item)
				res, err := ch.doCreate(ctx, r, body)
				if err != nil {
					msg, fields := classifyDoErr(err)
					results[i] = batchResult{Index: i, Error: msg, Fields: fields}
					return errBatchAborted
				}
				results[i] = batchResult{Index: i, Data: res}
			}
			return nil
		})

		if txErr != nil && !errors.Is(txErr, errBatchAborted) {
			log.Printf("crud: batch tx failed: %v", txErr)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
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
				body, hookErr := ch.runResponseHooks(r, res.Data)
				if hookErr != nil {
					// Degrade this item to its id, the batch is committed, and
					// a 500 would lose the ids of rows already in the table.
					log.Printf("crud: after-get hook failed on batch response, returning id only: %v", hookErr)
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
		if err := enforceJSONContentType(r); err != nil {
			writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		if !ch.requireScope(w, r, opUpdate) {
			return
		}
		limitJSONBody(w, r)
		var req batchUpdateRequest
		if err := decodeBatchEnvelope(r, &req, "items"); err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(req.Items) == 0 {
			writeJSONError(w, http.StatusBadRequest, "items must be non-empty")
			return
		}
		if len(req.Items) > MaxBatchSize {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("batch size %d exceeds max %d", len(req.Items), MaxBatchSize))
			return
		}

		results := initSkipped(len(req.Items))
		txErr := ch.inTx(r.Context(), func(ctx context.Context, ch *CrudHandler) error {
			for i, item := range req.Items {
				body := ch.unconvertMapKeys(item)
				idVal, ok := body[ch.PrimaryKey]
				if !ok {
					results[i] = batchResult{Index: i, Error: fmt.Sprintf("missing %q", ch.PrimaryKey)}
					return errBatchAborted
				}
				id, ok := idVal.(string)
				if !ok {
					id = fmt.Sprintf("%v", idVal)
				}
				delete(body, ch.PrimaryKey)
				res, err := ch.doUpdate(ctx, r, id, body)
				if err != nil {
					msg, fields := classifyDoErr(err)
					results[i] = batchResult{Index: i, Error: msg, Fields: fields}
					return errBatchAborted
				}
				results[i] = batchResult{Index: i, Data: res}
			}
			return nil
		})

		if txErr != nil && !errors.Is(txErr, errBatchAborted) {
			log.Printf("crud: batch tx failed: %v", txErr)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
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
				body, hookErr := ch.runResponseHooks(r, res.Data)
				if hookErr != nil {
					// Degrade this item to its id, the batch is committed, and
					// a 500 would lose the ids of rows already in the table.
					log.Printf("crud: after-get hook failed on batch response, returning id only: %v", hookErr)
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
		if err := enforceJSONContentType(r); err != nil {
			writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		if !ch.requireScope(w, r, opDelete) {
			return
		}
		limitJSONBody(w, r)
		var req batchDeleteRequest
		if err := decodeBatchEnvelope(r, &req, "ids"); err != nil {
			if errors.Is(err, errBodyTooLarge) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(req.IDs) == 0 {
			writeJSONError(w, http.StatusBadRequest, "ids must be non-empty")
			return
		}
		if len(req.IDs) > MaxBatchSize {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("batch size %d exceeds max %d", len(req.IDs), MaxBatchSize))
			return
		}

		results := initSkipped(len(req.IDs))
		txErr := ch.inTx(r.Context(), func(ctx context.Context, ch *CrudHandler) error {
			for i, id := range req.IDs {
				if err := ch.doDelete(ctx, r, id); err != nil {
					msg, fields := classifyDoErr(err)
					results[i] = batchResult{Index: i, Error: msg, Fields: fields}
					return errBatchAborted
				}
				results[i] = batchResult{Index: i, Data: map[string]any{ch.convertKey(ch.PrimaryKey): id}}
			}
			return nil
		})

		if txErr != nil && !errors.Is(txErr, errBatchAborted) {
			log.Printf("crud: batch tx failed: %v", txErr)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
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
