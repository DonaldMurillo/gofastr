package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// The upload bridge. A trigger rendered with Upload.Attrs carries
// data-local-send="<coll>[:<key>][,…]" and data-fui-rpc-with=
// "local-bridge"; rpc.js loads the module before the fetch and its
// request hook attaches the named records as the reserved field
// __local: {"<coll>": [{"k": key, "v": value}, …]} in a JSON body, the
// form field __local in a form body, or a fresh JSON body when the
// trigger had none. Upload.Wrap reads the field on the server, refuses
// anything the declaration did not name, enforces the caps, strips the
// field so the wrapped handler decodes the body it always did, and
// puts the records on the request context for Get and List.

// The reserved field name, on both sides.
const uploadField = "__local"

// Upload body bounds. The default is DERIVED from what the Send named:
// per item the most that item can put on the wire, summed,
// plus UploadBodySlack for the rest of the body. There is no floor: a
// flat one meant a collection capped at 512 bytes x 10 records, 5 KiB
// of records, still declared a 1 MiB bound, so the browser's
// fail-closed pre-flight could never fire and the server accepted a
// megabyte for a 5 KiB collection. A bound the declaration cannot
// reach is not a bound. Raise it explicitly with Upload.Max.
const (
	UploadMaxBytesLimit = 8 << 20
	// UploadBodySlack is what the rest of the request may add: the form
	// fields or JSON the trigger was already sending. A trigger that
	// carries a large body of its own alongside its records raises the
	// bound with Upload.Max.
	UploadBodySlack = 2 << 10
	// UploadRecordOverhead is the wire cost of ONE record around its
	// value: {"k":"<key>","v":} and the comma. KeyMaxLen is 256, which
	// no real key spends; 64 covers a key a human wrote plus the
	// punctuation, and Upload.Max covers the app that wants more.
	UploadRecordOverhead = 64
)

// ErrTooLarge is a record, collection or header over its cap: the one
// refusal that decides a status code (413 where every other refusal is
// a 400 that says why in its text).
var ErrTooLarge = errors.New("local: over the size cap")

// Upload is one declaration of what accompanies a request.
type Upload struct {
	store *Store
	items []sendItem
	max   int
}

// Send declares the collections (whole) or records (Collection.Key)
// that ride on the requests of the trigger rendered with Attrs, and
// that Wrap accepts on the server. Every item must belong to one
// store; an empty Send panics, and so does an item another item of
// the same Send already covers (a key beside its whole collection, in
// either order, or the same item twice): the bound would count the
// records twice, and the declaration would say two things.
func Send(items ...Sendable) *Upload {
	if len(items) == 0 {
		panic("local: Send needs at least one collection or key")
	}
	u := &Upload{}
	declared := 0
	for _, it := range items {
		si := it.sendItem()
		if u.store == nil {
			u.store = si.def.store
		} else if u.store != si.def.store {
			panic(fmt.Sprintf("local: Send mixes stores %q and %q — one store per upload", u.store.app, si.def.store.app))
		}
		for _, prev := range u.items {
			if prev.def == si.def && (prev.key == "" || si.key == "" || prev.key == si.key) {
				panic(fmt.Sprintf("local: Send names %s and %s; the second is already covered by the first", prev, si))
			}
		}
		u.items = append(u.items, si)
		declared += si.bound()
	}
	u.max = clampUpload(declared + UploadBodySlack)
	return u
}

// String names the item the way Attrs spells it, for a panic.
func (si sendItem) String() string {
	if si.key == "" {
		return "collection " + strconv.Quote(si.def.name)
	}
	return "key " + strconv.Quote(si.def.name+":"+si.key)
}

// bound is the most one Send item can put on the wire. A whole
// collection is bounded by the SMALLER of its two caps: ten records of
// 512 bytes never reach the 1 MiB MaxBytes the declaration defaulted
// to, and summing MaxBytes alone is how the derived bound came out 200
// times the size the declaration allows.
//
// The product is taken in int64: MaxRecordsLimit × MaxRecordBytesLimit
// is 2^37, past a 32-bit int, and a wrapped product is a bound the
// browser's pre-flight can never fire on. It is clamped to
// UploadMaxBytesLimit before it narrows, so no platform sees the wrap.
func (si sendItem) bound() int {
	if si.key != "" {
		return si.def.maxRecord + UploadRecordOverhead
	}
	n := int64(si.def.maxRecords) * int64(si.def.maxRecord)
	if int64(si.def.maxBytes) < n {
		n = int64(si.def.maxBytes)
	}
	n += int64(si.def.maxRecords) * UploadRecordOverhead
	if n > UploadMaxBytesLimit {
		n = UploadMaxBytesLimit
	}
	return int(n)
}

// clampUpload holds a derived bound under the ceiling Max accepts.
// There is no floor: one that could exceed the sum would hand the
// browser a bound its records can never reach, which is the pre-flight
// refusal not firing.
func clampUpload(n int) int {
	if n > UploadMaxBytesLimit {
		return UploadMaxBytesLimit
	}
	return n
}

// Max caps the wrapped request's whole body (the records and the rest)
// in bytes, replacing the bound Send derived from the declaration;
// outside (0, UploadMaxBytesLimit] panics.
func (u *Upload) Max(bytes int) *Upload {
	if bytes <= 0 || bytes > UploadMaxBytesLimit {
		panic(fmt.Sprintf("local: Upload.Max %d is outside (0, %d]", bytes, UploadMaxBytesLimit))
	}
	u.max = bytes
	return u
}

// Attrs returns the attributes the RPC trigger (a <form data-fui-rpc>
// or a button) carries: data-local-store, data-local-send,
// data-local-max and data-fui-rpc-with. Merge them into the trigger's
// attribute map, or use Merge.
//
// data-local-max is the body bound above, so the browser can refuse an
// oversized upload with gofastr:local-error{reason:"size"} instead of
// sending it and reading a bare 413 the page cannot see.
func (u *Upload) Attrs() map[string]string {
	parts := make([]string, 0, len(u.items))
	for _, it := range u.items {
		if it.key == "" {
			parts = append(parts, it.def.name)
		} else {
			parts = append(parts, it.def.name+":"+it.key)
		}
	}
	return map[string]string{
		"data-local-store":  u.store.app,
		"data-local-send":   strings.Join(parts, ","),
		"data-local-max":    strconv.Itoa(u.max),
		"data-fui-rpc-with": BridgeName,
	}
}

// Merge returns attrs plus Attrs, panicking when attrs names a GET
// method: an upload rides a mutating request.
func (u *Upload) Merge(attrs map[string]string) map[string]string {
	if strings.EqualFold(attrs["data-fui-rpc-method"], http.MethodGet) {
		panic("local: Send on a GET trigger — an upload rides a mutating request")
	}
	out := make(map[string]string, len(attrs)+4)
	for k, v := range attrs {
		out[k] = v
	}
	for k, v := range u.Attrs() {
		out[k] = v
	}
	return out
}

// allowed reports whether coll/key is inside the declaration.
func (u *Upload) allowed(coll, key string) *collectionDef {
	for _, it := range u.items {
		if it.def.name != coll {
			continue
		}
		if it.key == "" || it.key == key {
			return it.def
		}
	}
	return nil
}

// limitFor is the most records the DECLARATION lets this collection
// deliver, which is not always its cap: a Send that names two keys of
// a collection lets two records through, not the collection's cap.
func (u *Upload) limitFor(coll string, def *collectionDef) int {
	n := 0
	for _, it := range u.items {
		if it.def.name != coll {
			continue
		}
		if it.key == "" {
			return def.maxRecords
		}
		n++
	}
	if n > def.maxRecords {
		return def.maxRecords
	}
	return n
}

// uploadRecord is one wire record.
type uploadRecord struct {
	K string          `json:"k"`
	V json.RawMessage `json:"v"`
}

// records is what Wrap puts on the context: raw JSON by app, then
// collection, then key. A map keyed by app so two stores' uploads on
// one request do not collide.
type records map[string]map[string]map[string]json.RawMessage

type recordsKey struct{}

func withRecords(ctx context.Context, app string, recs map[string]map[string]json.RawMessage) context.Context {
	all, _ := ctx.Value(recordsKey{}).(records)
	next := records{}
	for a, m := range all {
		next[a] = m
	}
	next[app] = recs
	return context.WithValue(ctx, recordsKey{}, next)
}

func recordsFrom(ctx context.Context, app string) map[string]map[string]json.RawMessage {
	all, _ := ctx.Value(recordsKey{}).(records)
	return all[app]
}

// wrappedKey marks a context that went through some store's Upload.Wrap,
// on EVERY path through it: a GET, a body the field cannot ride on and
// a body without the field all reach the handler with no records, and
// "the wrapper ran and the browser sent nothing" has to be
// distinguishable from "nobody wrapped this handler". Only the dev
// warning in read.go reads it.
type wrappedKey struct{}

func markWrapped(ctx context.Context, app string) context.Context {
	prev, _ := ctx.Value(wrappedKey{}).(map[string]bool)
	next := map[string]bool{app: true}
	for a := range prev {
		next[a] = true
	}
	return context.WithValue(ctx, wrappedKey{}, next)
}

func wrappedFor(ctx context.Context, app string) bool {
	m, _ := ctx.Value(wrappedKey{}).(map[string]bool)
	return m[app]
}

// parse validates the reserved field's value against the declaration
// and the caps. It returns app-scoped records, or an error that maps
// to 400 (undeclared, bad key, malformed) or 413 (ErrTooLarge).
func (u *Upload) parse(raw []byte) (map[string]map[string]json.RawMessage, error) {
	var wire map[string][]uploadRecord
	if err := handler.UnmarshalStrict(raw, &wire); err != nil {
		return nil, fmt.Errorf("local: %s is not {collection: [{k, v}]}: %w", uploadField, err)
	}
	out := map[string]map[string]json.RawMessage{}
	for coll, recs := range wire {
		// The collection, once, before any record: a name the store
		// never declared is refused even when it carries no records,
		// which the old per-first-record pre-check let through as an
		// empty collection on the context.
		if u.store.defOf(coll) == nil {
			return nil, fmt.Errorf("local: undeclared collection %q", coll)
		}
		total := 0
		byKey := map[string]json.RawMessage{}
		for _, rec := range recs {
			if !validRecordKey(rec.K) {
				return nil, fmt.Errorf("local: invalid key %q in %q", rec.K, coll)
			}
			def := u.allowed(coll, rec.K)
			if def == nil {
				return nil, fmt.Errorf("local: undeclared key %q/%q", coll, rec.K)
			}
			if rec.V == nil {
				return nil, fmt.Errorf("local: record %q/%q has no value", coll, rec.K)
			}
			v := bytes.TrimSpace(rec.V)
			if len(v) > def.maxRecord {
				return nil, fmt.Errorf("%w: record %q/%q is %d bytes, cap %d", ErrTooLarge, coll, rec.K, len(v), def.maxRecord)
			}
			if _, dup := byKey[rec.K]; dup {
				return nil, fmt.Errorf("local: record %q/%q appears twice", coll, rec.K)
			}
			byKey[rec.K] = v
			total += len(v)
			if limit := u.limitFor(coll, def); len(byKey) > limit {
				return nil, fmt.Errorf("%w: collection %q has more than %d records", ErrTooLarge, coll, limit)
			}
			if total > def.maxBytes {
				return nil, fmt.Errorf("%w: collection %q exceeds %d bytes", ErrTooLarge, coll, def.maxBytes)
			}
		}
		out[coll] = byKey
	}
	return out, nil
}

// Wrap returns h with the upload read off the request first. The body
// is bounded by Max (413 past it); a JSON body is decoded strictly
// (400 on a duplicate or case-folded key, the rule every /__gofastr
// endpoint keeps), the reserved field is removed and the rest is
// re-encoded so h decodes what it always did; a form body has the
// field removed from r.Form / r.PostForm / r.MultipartForm after
// parsing. A request with no reserved field passes through with no
// records. An undeclared collection or key is a 400; a record over a
// cap is a 413. h then sees the records through Get and List, and
// app.RequestFromContext works inside it.
func (u *Upload) Wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(markWrapped(app.WithRequest(r.Context(), r), u.store.app))
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Body == nil {
			h.ServeHTTP(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, int64(u.max))
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		var raw []byte
		var err error
		switch ct {
		case "application/json":
			raw, err = u.stripJSON(r)
		case "application/x-www-form-urlencoded", "multipart/form-data":
			raw, err = u.stripForm(r, ct)
		default:
			h.ServeHTTP(w, r)
			return
		}
		if err != nil {
			respondUploadError(w, err)
			return
		}
		if raw == nil {
			h.ServeHTTP(w, r)
			return
		}
		recs, err := u.parse(raw)
		if err != nil {
			respondUploadError(w, err)
			return
		}
		h.ServeHTTP(w, r.WithContext(withRecords(r.Context(), u.store.app, recs)))
	})
}

// HandlerFunc is Wrap over a plain function.
func (u *Upload) HandlerFunc(fn func(http.ResponseWriter, *http.Request)) http.Handler {
	return u.Wrap(http.HandlerFunc(fn))
}

func respondUploadError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	switch {
	case errors.Is(err, ErrTooLarge), errors.As(err, &maxErr):
		http.Error(w, "local upload over the size cap", http.StatusRequestEntityTooLarge)
	default:
		http.Error(w, "local upload refused: "+err.Error(), http.StatusBadRequest)
	}
}

// stripJSON reads the JSON body, lifts the reserved field out and
// replaces the body with the rest. nil, nil when the field is absent
// (the body is still replaced, byte for byte, so nothing was lost).
//
// When the field WAS present the rest is re-marshalled, so the wrapped
// handler sees semantically the same object with a different encoding:
// keys in Go's map order, no insignificant whitespace. A handler that
// hashes or signs the raw body must do it upstream of Wrap. Both
// r.ContentLength and the Content-Length HEADER are corrected: a stale
// header is what a downstream proxy, a middleware that re-reads the
// body, or a test that trusts it will believe over the reader.
func (u *Upload) stripJSON(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return nil, nil
	}
	var top map[string]json.RawMessage
	if err := handler.UnmarshalStrict(body, &top); err != nil {
		return nil, err
	}
	field, ok := top[uploadField]
	if !ok {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil, nil
	}
	delete(top, uploadField)
	rest, err := json.Marshal(top)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(rest))
	r.ContentLength = int64(len(rest))
	r.Header.Set("Content-Length", strconv.Itoa(len(rest)))
	return field, nil
}

// stripForm parses the form and lifts the reserved field out of it.
func (u *Upload) stripForm(r *http.Request, ct string) ([]byte, error) {
	var err error
	if ct == "multipart/form-data" {
		err = r.ParseMultipartForm(int64(u.max))
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		return nil, err
	}
	vals := r.PostForm[uploadField]
	if len(vals) == 0 {
		return nil, nil
	}
	if len(vals) > 1 {
		return nil, fmt.Errorf("local: %s appears %d times", uploadField, len(vals))
	}
	delete(r.PostForm, uploadField)
	delete(r.Form, uploadField)
	if r.MultipartForm != nil {
		delete(r.MultipartForm.Value, uploadField)
	}
	return []byte(vals[0]), nil
}

// sortedKeys is the deterministic order List uses.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
