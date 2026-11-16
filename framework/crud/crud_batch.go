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
const MaxBatchSize = 100

// decodeBatchEnvelope decodes a _batch body into v once its top-level
// object carries exactly the allowed keys, each once, spelled exactly.
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
	if err := handler.UnmarshalStrict(raw, v, handler.UseNumber()); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

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

func envelopeError(detail i18n.Phrase, params map[string]any) error {
	return &i18n.Error{Reason: reasonInvalidBody, Params: map[string]any{"detail": i18n.NewError(detail, params)}}
}

var errBatchAborted = errors.New("batch aborted")

// batchResult is one entry in a _batch response: Data, Error or Skipped.
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

// classifyDoErr reports one item's error as the single write refuses it.
func classifyDoErr(ctx context.Context, index int, err error, wireKeys ...func(string) string) batchResult {
	f := crudRefusal(ctx, err, wireKeys...)
	return batchResult{Index: index, Error: f.text, Reason: f.reason, Fields: f.fields, FieldReasons: f.fieldReasons}
}

func initSkipped(n int) []batchResult {
	out := make([]batchResult, n)
	for i := range out {
		out[i] = batchResult{Index: i, Skipped: true}
	}
	return out
}

// writeBatchResponse answers 200 when the batch committed, else 400 with
// no item Data.
func writeBatchResponse(w http.ResponseWriter, r *http.Request, resp BatchResponse) {
	status := http.StatusOK
	if !resp.Committed {
		scrubRolledBackData(resp.Results)
		status = http.StatusBadRequest
	}
	writeJSONStatus(w, r, status, resp)
}

func scrubRolledBackData(results []batchResult) {
	for i := range results {
		results[i].Data = nil
	}
}

// shapeBatchResults runs each committed item's body through the response
// hooks after commit, as the single-row routes do; a hidden row or a hook
// error answers the id only.
func (ch *CrudHandler) shapeBatchResults(r *http.Request, results []batchResult, hidden []bool) {
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
			logf("crud: after-get hook failed on batch response, returning id only: %v", hookErr)
			body = ch.identityOnly(res.Data)
		}
		results[i].Data = body
	}
}

type batchCreateRequest struct {
	Items []map[string]any `json:"items"`
}

// BatchCreate handles POST /{table}/_batch with {items:[...]}: every item
// in one transaction, the first error rolling all back, and results in
// input order.
func (ch *CrudHandler) BatchCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
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
		for i, item := range req.Items {
			if err := ch.itemKeyFoldsUnambiguous(item); err != nil {
				writeBatchItemError(w, r, i, err)
				return
			}
		}

		results := initSkipped(len(req.Items))
		hidden := make([]bool, len(req.Items))
		txErr := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			for i, item := range req.Items {
				body := ch.unconvertMapKeys(item)
				res, err := ch.doCreate(ctx, r, body)
				if err == nil {
					hidden[i], err = ch.readScopeHidesRow(ctx, res)
				}
				if err != nil {
					results[i] = classifyDoErr(ctx, i, err, ch.convertKey)
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

		if txErr == nil {
			ch.shapeBatchResults(r, results, hidden)
		}
		writeBatchResponse(w, r, BatchResponse{Committed: txErr == nil, Results: results})
	}
}

type batchUpdateRequest struct {
	Items []map[string]any `json:"items"`
}

// BatchUpdate handles PATCH /{table}/_batch with {items:[...]}, each item
// naming its record by key, as BatchCreate.
func (ch *CrudHandler) BatchUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
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
		for i, item := range req.Items {
			if err := ch.itemKeyFoldsUnambiguous(item); err != nil {
				writeBatchItemError(w, r, i, err)
				return
			}
		}

		results := initSkipped(len(req.Items))
		hidden := make([]bool, len(req.Items))
		txErr := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
			for i, item := range req.Items {
				body := ch.unconvertMapKeys(item)
				id, ok := entity.RowKey(body, ch.keyCols(), nil)
				if !ok {
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
					results[i] = classifyDoErr(ctx, i, err, ch.convertKey)
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

		if txErr == nil {
			ch.shapeBatchResults(r, results, hidden)
		}
		writeBatchResponse(w, r, BatchResponse{Committed: txErr == nil, Results: results})
	}
}

type batchDeleteRequest struct {
	IDs []string `json:"ids"`
}

// BatchDelete handles DELETE /{table}/_batch with {ids:[...]}, as
// BatchCreate.
func (ch *CrudHandler) BatchDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(withEventStamps(r.Context()))
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
					results[i] = classifyDoErr(ctx, i, err, ch.convertKey)
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
		writeBatchResponse(w, r, BatchResponse{Committed: txErr == nil, Results: results})
	}
}
