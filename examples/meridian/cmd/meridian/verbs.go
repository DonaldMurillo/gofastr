package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

// filterFlag binds one list flag to the query param it sets. String
// filters (the default) only set their param when non-empty; isBool
// filters (soft-delete trashed) set theirs to "true" when given.
type filterFlag struct {
	flag   string
	param  string
	help   string
	isBool bool
}

// runListVerb is the shared list body: the pagination/output flags, the
// entity's filter table (filters), the query string, and the JSON or
// table print.
func runListVerb(cmd, base string, filters []filterFlag, headers, keys []string, args []string) int {
	fs := newFlagSet(cmd)
	sortF := fs.String("sort", "", "sort field(s), comma-separated, - prefix for desc")
	page := fs.String("page", "", "page number (offset pagination)")
	limit := fs.String("limit", "", "page size")
	cursor := fs.String("cursor", "", "keyset cursor (from a prior response)")
	include := fs.String("include", "", "relations to eager-load (comma, dots for nesting)")
	fieldsF := fs.String("fields", "", "sparse field projection (comma-separated)")
	outF := fs.String("o", "json", "output format: json|table")
	var params paramFlags
	fs.Var(&params, "param", "extra query param key=value (repeatable)")
	strVals := make([]*string, len(filters))
	boolVals := make([]*bool, len(filters))
	for i, f := range filters {
		if f.isBool {
			boolVals[i] = fs.Bool(f.flag, false, f.help)
		} else {
			strVals[i] = fs.String(f.flag, "", f.help)
		}
	}
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	q := url.Values{}
	set := func(key, val string) {
		if val != "" {
			q.Set(key, val)
		}
	}
	set("sort", *sortF)
	set("page", *page)
	set("limit", *limit)
	set("cursor", *cursor)
	set("include", *include)
	set("fields", *fieldsF)
	for i, f := range filters {
		if f.isBool {
			if *boolVals[i] {
				q.Set(f.param, "true")
			}
		} else {
			set(f.param, *strVals[i])
		}
	}
	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
	path := base
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
		printListTable(headers, keys, resp.Data)
		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows, next cursor: %s\n", len(resp.Data), resp.Cursor)
		} else {
			fmt.Printf("page %d/%d, %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

// runGetVerb is the shared get body: take the positional id, GET
// base/{id}, print the record.
func runGetVerb(cmd, base string, args []string) int {
	id, rest, ok := takeID(cmd, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

// fieldKind is the flag type a mutation field declares.
type fieldKind int

const (
	fieldString fieldKind = iota
	fieldInt
	fieldFloat
	fieldBool
	fieldJSON
)

// mutationField describes one writable field behind create/update/patch:
// the CLI flag, the JSON wire key it sets, the flag type, and the usage
// line. Field names are data bound at generate time (string literals),
// never identifiers.
type mutationField struct {
	flag  string
	wire  string
	kind  fieldKind
	usage string
}

// runCreateVerb is the shared create body over the entity's field table.
func runCreateVerb(cmd, base string, fields []mutationField, args []string) int {
	return runMutationVerb(cmd, http.MethodPost, false, base, fields, args)
}

// runUpdateVerb is the shared update body over the entity's field table.
func runUpdateVerb(cmd, base string, fields []mutationField, args []string) int {
	return runMutationVerb(cmd, http.MethodPut, true, base, fields, args)
}

// runPatchVerb is the shared patch body over the entity's field table.
func runPatchVerb(cmd, base string, fields []mutationField, args []string) int {
	return runMutationVerb(cmd, http.MethodPatch, true, base, fields, args)
}

// runMutationVerb is the shared create/update/patch body: per-field flags
// from the entity's field table OR --json, sent as a presence-faithful map
// so explicit zero values survive. withID selects the item form:
// update/patch pop the positional id first and address base/{id}; create
// posts the collection itself.
func runMutationVerb(cmd, method string, withID bool, base string, fields []mutationField, args []string) int {
	id := ""
	if withID {
		var rest []string
		var ok bool
		if id, rest, ok = takeID(cmd, args); !ok {
			return 2
		}
		args = rest
	}
	fs := newFlagSet(cmd)
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	strVals := make([]*string, len(fields))
	intVals := make([]*int, len(fields))
	floatVals := make([]*float64, len(fields))
	boolVals := make([]*bool, len(fields))
	for i, f := range fields {
		switch f.kind {
		case fieldInt:
			intVals[i] = fs.Int(f.flag, 0, f.usage)
		case fieldFloat:
			floatVals[i] = fs.Float64(f.flag, 0, f.usage)
		case fieldBool:
			boolVals[i] = fs.Bool(f.flag, false, f.usage)
		default: // string and json-typed fields both arrive as strings
			strVals[i] = fs.String(f.flag, "", f.usage)
		}
	}
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		for i, f := range fields {
			if f.flag != name {
				continue
			}
			switch f.kind {
			case fieldInt:
				body[f.wire] = *intVals[i]
			case fieldFloat:
				body[f.wire] = *floatVals[i]
			case fieldBool:
				body[f.wire] = *boolVals[i]
			case fieldJSON:
				var v any
				if err := json.Unmarshal([]byte(*strVals[i]), &v); err != nil {
					return fmt.Errorf("--%s: %w", f.flag, err)
				}
				body[f.wire] = v
			default:
				body[f.wire] = *strVals[i]
			}
		}
		return nil
	})
	if code != 0 {
		return code
	}
	path := base
	if withID {
		path += "/" + url.PathEscape(id)
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, method, path, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

// runDeleteVerb is the shared delete body: take the positional id,
// DELETE base/{id}, confirm on stdout.
func runDeleteVerb(cmd, base string, args []string) int {
	id, rest, ok := takeID(cmd, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, base+"/"+url.PathEscape(id), nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %s\n", id)
	return 0
}

// runTransitionVerb is the shared move body: take the positional id,
// POST base/{id}/transitions/{key}, print the moved record. The route
// takes no payload but requires the JSON content type (its cross-site
// gate), so the request carries an empty JSON body.
func runTransitionVerb(cmd, base, key string, args []string) int {
	id, rest, ok := takeID(cmd, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	var out singleResponse
	path := base + "/" + url.PathEscape(id) + "/transitions/" + url.PathEscape(key)
	if err := g.client.Do(g.ctx, http.MethodPost, path, map[string]any{}, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

// runBatchJSONVerb is the shared batch-create/batch-update body: send a
// --json array through the atomic _batch route wrapped into the
// {items: [...]} envelope. A rolled-back batch prints its
// {committed, results[]} envelope and exits 1.
func runBatchJSONVerb(cmd, base, method string, args []string) int {
	fs := newFlagSet(cmd)
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	resp, code := doBatch(g, method, base+"/_batch", map[string]any{"items": items})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runBatchDeleteVerb deletes the positional ids in one transaction. Ids
// may appear before or after flags: flag.Parse stops at the first
// positional, so the trailing ones are collected from fs.Args().
func runBatchDeleteVerb(cmd, base string, args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	for _, id := range fs.Args() {
		if id != "" && id[0] == '-' {
			fmt.Fprintln(os.Stderr, binaryName+" "+cmd+": flags must precede trailing ids (got "+id+" after an id)")
			return 2
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "usage: "+binaryName+" "+cmd+" <id> [id...]")
		return 2
	}
	resp, code := doBatch(g, http.MethodDelete, base+"/_batch", map[string]any{"ids": ids})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runWatchVerb is the shared watch body: stream the entity's live event
// feed until interrupted; each event is one JSON line on stdout. watch is
// the typed client method expression ((*client.Client).Watch<Entity>),
// bound by each entity's wrapper.
func runWatchVerb(cmd string, watch func(c *client.Client, ctx context.Context, fn func(event string, data []byte) error) error, args []string) int {
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	// g.ctx is already signal-cancellable: parseGlobals built it with
	// signal.NotifyContext, so Ctrl-C cancels the stream here too.
	err := watch(g.client, g.ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%q,\"data\":%s}\n", event, data)
		return nil
	})
	if err != nil && g.ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}
