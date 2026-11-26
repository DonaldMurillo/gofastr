package crud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/internal/bodylimit"
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
		return err
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

// batchSpec is one _batch op's half of the shared runBatch scaffold:
// what differs between create, update and delete.
type batchSpec struct {
	op         crudOp                     // the scope gate the op runs
	envelope   any                        // pointer to the op's decode target
	key        string                     // the envelope's one allowed key
	empty      i18n.Phrase                // the refusal for an empty list
	count      func() int                 // the decoded item count
	normalize  func()                     // item fixups before the size refusals; nil for ids
	checkItems func(r *http.Request) bool // per-item refusal after them; nil for ids; false = already answered
	event      string                     // the event each committed item emits
	shape      bool                       // run the response hooks over the committed bodies
	tx         func(ctx context.Context, r *http.Request, ch *CrudHandler, results []batchResult, hidden []bool) error
}

// runBatch is the shared _batch scaffold: event stamps, no-store, the
// JSON and scope gates, the body limit, the strict envelope decode, the
// empty and MaxBatchSize refusals, the transaction, and on commit the
// event loop, response hooks and envelope.
func (ch *CrudHandler) runBatch(w http.ResponseWriter, r *http.Request, spec batchSpec) {
	r = r.WithContext(withEventStamps(r.Context()))
	w.Header().Set("Cache-Control", "no-store")
	if err := enforceJSONOnly(r); err != nil {
		writeContentTypeError(w, r, err)
		return
	}
	if !ch.requireScope(w, r, spec.op) {
		return
	}
	limitJSONBody(w, r)
	if err := decodeBatchEnvelope(r, spec.envelope, spec.key); err != nil {
		if errors.Is(err, errBodyTooLarge) {
			status, phrase := bodylimit.TooLargeRefusal()
			writeJSONError(w, r, status, phrase)
			return
		}
		writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidBody)
		return
	}
	if spec.normalize != nil {
		spec.normalize()
	}
	n := spec.count()
	if n == 0 {
		writeJSONError(w, r, http.StatusBadRequest, spec.empty)
		return
	}
	if n > MaxBatchSize {
		writeJSONError(w, r, http.StatusBadRequest, reasonBatchTooLarge, map[string]any{"size": n, "max": MaxBatchSize})
		return
	}
	if spec.checkItems != nil && !spec.checkItems(r) {
		return
	}
	results := initSkipped(n)
	var hidden []bool
	if spec.shape {
		hidden = make([]bool, n)
	}
	txErr := ch.inTx(WithAuditRequest(r.Context(), r), func(ctx context.Context, ch *CrudHandler) error {
		return spec.tx(ctx, r, ch, results, hidden)
	})
	if txErr != nil && !errors.Is(txErr, errBatchAborted) {
		logf("crud: batch tx failed: %v", txErr)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	if txErr == nil {
		for _, res := range results {
			if res.Data != nil {
				ch.EmitEvent(r.Context(), spec.event, res.Data)
			}
		}
	}
	if txErr == nil && spec.shape {
		ch.shapeBatchResults(r, results, hidden)
	}
	writeBatchResponse(w, r, BatchResponse{Committed: txErr == nil, Results: results})
}

type batchCreateRequest struct {
	Items []map[string]any `json:"items"`
}

// checkBatchItems refuses, by index, an item that is not an object or
// whose keys fold onto one column.
func (ch *CrudHandler) checkBatchItems(w http.ResponseWriter, r *http.Request, items []map[string]any) bool {
	for i, item := range items {
		if item == nil {
			writeBatchItemError(w, r, i, i18n.NewError(reasonNotObject))
			return false
		}
		if err := ch.itemKeyFoldsUnambiguous(item); err != nil {
			writeBatchItemError(w, r, i, err)
			return false
		}
	}
	return true
}

// BatchCreate handles POST /{table}/_batch with {items:[...]}: every item
// in one transaction, the first error rolling all back, and results in
// input order.
func (ch *CrudHandler) BatchCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req batchCreateRequest
		ch.runBatch(w, r, batchSpec{
			op: opCreate, envelope: &req, key: "items", empty: reasonItemsEmpty,
			count:     func() int { return len(req.Items) },
			normalize: func() { normalizeItemNumbers(req.Items) },
			checkItems: func(r *http.Request) bool {
				return ch.checkBatchItems(w, r, req.Items)
			},
			event: event.EntityCreated,
			shape: true,
			tx: func(ctx context.Context, r *http.Request, ch *CrudHandler, results []batchResult, hidden []bool) error {
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
			},
		})
	}
}

type batchUpdateRequest struct {
	Items []map[string]any `json:"items"`
}

// BatchUpdate handles PATCH /{table}/_batch with {items:[...]}, each item
// naming its record by key, as BatchCreate.
func (ch *CrudHandler) BatchUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req batchUpdateRequest
		ch.runBatch(w, r, batchSpec{
			op: opUpdate, envelope: &req, key: "items", empty: reasonItemsEmpty,
			count:     func() int { return len(req.Items) },
			normalize: func() { normalizeItemNumbers(req.Items) },
			checkItems: func(r *http.Request) bool {
				return ch.checkBatchItems(w, r, req.Items)
			},
			event: event.EntityUpdated,
			shape: true,
			tx: func(ctx context.Context, r *http.Request, ch *CrudHandler, results []batchResult, hidden []bool) error {
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
			},
		})
	}
}

type batchDeleteRequest struct {
	IDs []string `json:"ids"`
}

// BatchDelete handles DELETE /{table}/_batch with {ids:[...]}, as
// BatchCreate.
func (ch *CrudHandler) BatchDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req batchDeleteRequest
		ch.runBatch(w, r, batchSpec{
			op: opDelete, envelope: &req, key: "ids", empty: reasonIDsEmpty,
			count: func() int { return len(req.IDs) },
			event: event.EntityDeleted,
			tx: func(ctx context.Context, r *http.Request, ch *CrudHandler, results []batchResult, _ []bool) error {
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
			},
		})
	}
}
