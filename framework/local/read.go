package local

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/config"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Reading browser-held records on the server. Two sources, both
// explicit:
//
//   - the records an Upload.Wrap put on the request context (the
//     __local field of an RPC body), and
//   - the cookies a Mirror collection keeps, read off the request in
//     the context (uihost installs it for screens with app.WithRequest;
//     Upload.Wrap installs it for handlers).
//
// A context record wins over a cookie for the same key. Everything is
// a client hint: validated against the declaration (collection, key,
// size, JSON shape into T), never trusted. A value that fails to
// decode into T is an error, not a zero value marching on. The decode
// into T is the strict one every /__gofastr endpoint uses: a struct T
// refuses a field it does not declare, and a duplicate key is refused
// at every level, for a cookie and an upload alike.
//
// The two sources are NOT the same evidence, so every read says which
// one answered. An uploaded record arrived on a request whose trigger
// declared it and whose handler is wrapped by the Upload that declared
// it; a mirror cookie is a value any script on the origin can write and
// any client can forge, sitting in the request whether the handler
// asked for it or not. A handler that treats them alike has an
// unauthenticated write channel it did not mean to open, which is why
// Source is a return value and not a doc note.

// The cookie namespace, one segment per record, component-encoded by
// the browser: gofastr.local.<app>.<collection>.<key>.
const cookiePrefix = "gofastr.local."

// Source says where a record the request carried came from.
type Source string

const (
	// SourceNone is "the request carried nothing for this key".
	SourceNone Source = ""
	// SourceUpload is a record an Upload.Wrap read off the request body,
	// declared by the trigger and accepted by the wrapper's own
	// validation.
	SourceUpload Source = "upload"
	// SourceMirror is a mirror cookie: a CLIENT HINT. Any script on the
	// origin can write it, any client can forge it, and it rides every
	// request whether the handler wanted it or not. Treat it the way you
	// would treat a query parameter, never as proof of anything.
	SourceMirror Source = "mirror"
)

// Found reports whether the request carried the record at all.
func (s Source) Found() bool { return s != SourceNone }

// Record is one key/value pair of a collection, with where it came
// from.
type Record[T any] struct {
	Key    string
	Value  T
	Source Source
}

// carried is every record the request carried for one store, by
// collection and key, as JSON, with where each came from.
type carried struct {
	recs map[string]map[string]json.RawMessage
	srcs map[string]map[string]Source
}

// carriedBy gathers every record the request carried for s: the
// uploaded ones and the mirrored cookies. List reads it, and so does
// Get for a mirrored collection, so the caps counted here hold for
// both.
func carriedBy(ctx context.Context, s *Store) *carried {
	out := &carried{recs: map[string]map[string]json.RawMessage{}, srcs: map[string]map[string]Source{}}
	put := func(coll, key string, raw json.RawMessage, src Source) {
		if out.recs[coll] == nil {
			out.recs[coll] = map[string]json.RawMessage{}
			out.srcs[coll] = map[string]Source{}
		}
		out.recs[coll][key] = raw
		out.srcs[coll][key] = src
	}
	if r := app.RequestFromContext(ctx); r != nil {
		// The per-record cap is checked in decodeCookie; the collection's
		// caps are counted here. A cookie past MaxRecords or MaxBytes is
		// ignored, not an error: the cookie is a client hint, and the
		// browser refuses the same write, so the extras are only ever a
		// forged header. The count is per collection, in cookie order.
		counts := map[string]int{}
		sizes := map[string]int{}
		for _, c := range r.Cookies() {
			coll, key, raw, ok := s.decodeCookie(c)
			if !ok {
				continue
			}
			def := s.defOf(coll)
			if _, seen := out.recs[coll][key]; !seen {
				if counts[coll] >= def.maxRecords || sizes[coll]+len(raw) > def.maxBytes {
					continue
				}
				counts[coll]++
				sizes[coll] += len(raw)
			}
			put(coll, key, raw, SourceMirror)
		}
	}
	// The upload wins for a key both sources carried: it is the one the
	// trigger declared and the wrapper validated.
	for coll, byKey := range recordsFrom(ctx, s.app) {
		for k, v := range byKey {
			put(coll, k, v, SourceUpload)
		}
	}
	return out
}

// decodeCookie recognises a mirror cookie of this store: the name is
// the namespace plus one component-encoded "<app>.<collection>.<key>",
// the value the component-encoded JSON text. Anything else is
// ignored: another app's, an undeclared or unmirrored collection, a
// bad key, a value over the record cap, JSON the upload would refuse
// (a duplicate or case-folded key at any level, more than one value).
// The collection's caps are counted by the callers.
func (s *Store) decodeCookie(c *http.Cookie) (coll, key string, raw json.RawMessage, ok bool) {
	if !strings.HasPrefix(c.Name, cookiePrefix) {
		return "", "", nil, false
	}
	name, err := url.PathUnescape(c.Name[len(cookiePrefix):])
	if err != nil || !strings.HasPrefix(name, s.app+".") {
		return "", "", nil, false
	}
	rest := name[len(s.app)+1:]
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 {
		return "", "", nil, false
	}
	coll, key = rest[:dot], rest[dot+1:]
	def := s.defOf(coll)
	if def == nil || !def.mirror || !validRecordKey(key) {
		return "", "", nil, false
	}
	text, err := url.PathUnescape(c.Value)
	if err != nil || len(text) > def.maxRecord || !validStrict([]byte(text)) {
		return "", "", nil, false
	}
	return coll, key, json.RawMessage(text), true
}

// validStrict is the validity the upload's parse applies, on a cookie:
// json.Valid accepts {"a":1,"a":2}, which reads two ways, and a stream
// of two values. The same bytes through the strict decoder into a raw
// message settle the question the same way for both sources.
func validStrict(text []byte) bool {
	var raw json.RawMessage
	return handler.UnmarshalStrict(text, &raw) == nil
}

// Reading outside Upload.Wrap is the quiet failure this package has: Get
// and List answer an empty result, the handler decides the browser sent
// nothing, and the missing line is one the compiler cannot ask for. A
// non-mirrored collection can ONLY arrive through the wrapper, so a read
// of one on a request that was never wrapped is always the mistake and
// never a browser with an empty store. Under GOFASTR_DEV, the flag the
// dev loop sets and the one framework/dev gates livereload on, say so
// once per collection; in production this costs one atomic read.
var unwrappedWarned sync.Map // "<app>/<collection>" -> struct{}

func warnUnwrapped(ctx context.Context, def *collectionDef, call string) {
	if def.mirror || !config.EnvBool("GOFASTR_DEV") {
		return
	}
	if wrappedFor(ctx, def.store.app) {
		return
	}
	k := def.store.app + "/" + def.name
	if _, dup := unwrappedWarned.LoadOrStore(k, struct{}{}); dup {
		return
	}
	slog.Default().Warn("local: "+call+" outside Upload.Wrap answers nothing",
		"app", def.store.app, "collection", def.name,
		"fix", "wrap the handler: local.Send("+def.name+"…).HandlerFunc(h), or .Wrap(h)")
}

// Get returns record key of c as the request carried it: from the
// upload first, then from the mirror cookie. The Source says which, and
// is SourceNone when the request carried nothing for the key (src.Found()
// reads as the old boolean). err reports a record that does not decode
// into T.
//
// SourceMirror is a client hint. Do not authorise on it.
func Get[T any](ctx context.Context, c *Collection[T], key string) (value T, src Source, err error) {
	raw, src := rawFor(ctx, c.def, key)
	if !src.Found() {
		warnUnwrapped(ctx, c.def, "Get")
		return value, SourceNone, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, src, fmt.Errorf("local: record %q/%q does not decode into %T: %w", c.def.name, key, value, err)
	}
	return value, src, nil
}

// List returns every record of c the request carried, sorted by key,
// each carrying its Source. A record that does not decode into T fails
// the whole list. A list can mix the two sources, and a caller that
// authorises on any of it must read Source per record.
func List[T any](ctx context.Context, c *Collection[T]) ([]Record[T], error) {
	from := carriedBy(ctx, c.def.store)
	all := from.recs[c.def.name]
	if len(all) == 0 {
		warnUnwrapped(ctx, c.def, "List")
	}
	out := make([]Record[T], 0, len(all))
	for _, k := range sortedKeys(all) {
		var v T
		if err := json.Unmarshal(all[k], &v); err != nil {
			return nil, fmt.Errorf("local: record %q/%q does not decode into %T: %w", c.def.name, k, v, err)
		}
		out = append(out, Record[T]{Key: k, Value: v, Source: from.srcs[c.def.name][k]})
	}
	return out, nil
}

func rawFor(ctx context.Context, def *collectionDef, key string) (json.RawMessage, Source) {
	if v, ok := recordsFrom(ctx, def.store.app)[def.name][key]; ok {
		return v, SourceUpload
	}
	if !def.mirror {
		return nil, SourceNone
	}
	// The whole gather, not a scan for the one cookie: the collection's
	// caps are counted over every cookie, and a Get past them must see
	// what List sees.
	from := carriedBy(ctx, def.store)
	if v, ok := from.recs[def.name][key]; ok {
		return v, from.srcs[def.name][key]
	}
	return nil, SourceNone
}
