package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func plansCommands() []command {
	return []command{
		{name: "plans list", summary: "list plans (filters, sort, pagination)", run: runPlansList},
		{name: "plans get", summary: "fetch one record by id", run: runPlansGet},
		{name: "plans create", summary: "create a record from field flags or --json", run: runPlansCreate},
		{name: "plans update", summary: "update fields on a record (PUT)", run: runPlansUpdate},
		{name: "plans patch", summary: "patch fields on a record (PATCH)", run: runPlansPatch},
		{name: "plans delete", summary: "delete a record by id", run: runPlansDelete},
		{name: "plans batch-create", summary: "create up to 100 records atomically (--json array)", run: runPlansBatchCreate},
		{name: "plans batch-update", summary: "patch up to 100 records atomically (--json array)", run: runPlansBatchUpdate},
		{name: "plans batch-delete", summary: "delete ids atomically (positional ids)", run: runPlansBatchDelete},
		{name: "plans watch", summary: "stream live create/update/delete events (SSE)", run: runPlansWatch},
	}
}

func runPlansList(args []string) int {
	fs := newFlagSet("plans list")
	sortF := fs.String("sort", "", "sort field(s), comma-separated, - prefix for desc")
	page := fs.String("page", "", "page number (offset pagination)")
	limit := fs.String("limit", "", "page size")
	cursor := fs.String("cursor", "", "keyset cursor (from a prior response)")
	include := fs.String("include", "", "relations to eager-load (comma, dots for nesting)")
	fieldsF := fs.String("fields", "", "sparse field projection (comma-separated)")
	outF := fs.String("o", "json", "output format: json|table")
	var params paramFlags
	fs.Var(&params, "param", "extra query param key=value (repeatable)")
	fltName := fs.String("name", "", "filter: name equals (comma list = IN)")
	fltNameLike := fs.String("name-like", "", "filter: name contains")
	fltSlug := fs.String("slug", "", "filter: slug equals (comma list = IN)")
	fltSlugLike := fs.String("slug-like", "", "filter: slug contains")
	fltPrice := fs.String("price", "", "filter: price equals (comma list = IN)")
	fltPriceGT := fs.String("price-gt", "", "filter: price gt")
	fltPriceGTE := fs.String("price-gte", "", "filter: price gte")
	fltPriceLT := fs.String("price-lt", "", "filter: price lt")
	fltPriceLTE := fs.String("price-lte", "", "filter: price lte")
	fltInterval := fs.String("interval", "", "filter: interval equals (comma list = IN) [month|year]")
	fltActive := fs.String("active", "", "filter: active equals (comma list = IN)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
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
	set("name", *fltName)
	set("name_like", *fltNameLike)
	set("slug", *fltSlug)
	set("slug_like", *fltSlugLike)
	set("price", *fltPrice)
	set("price_gt", *fltPriceGT)
	set("price_gte", *fltPriceGTE)
	set("price_lt", *fltPriceLT)
	set("price_lte", *fltPriceLTE)
	set("interval", *fltInterval)
	set("active", *fltActive)
	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
	path := "/plans"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
		printListTable([]string{"id", "name", "slug", "price", "interval", "active"}, []string{"id", "name", "slug", "price", "interval", "active"}, resp.Data)
		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows — next cursor: %s\n", len(resp.Data), resp.Cursor)
		} else {
			fmt.Printf("page %d/%d — %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

func runPlansGet(args []string) int {
	id, rest, ok := takeID("plans get", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("plans get")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, "/plans/"+id, nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPlansCreate(args []string) int {
	fs := newFlagSet("plans create")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldName := fs.String("name", "", "name (string)")
	fldSlug := fs.String("slug", "", "slug (string)")
	fldPrice := fs.String("price", "", "price (decimal)")
	fldInterval := fs.String("interval", "", "interval (enum) [month|year]")
	fldActive := fs.Bool("active", false, "active (bool)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "name":
			body["name"] = *fldName
		case "slug":
			body["slug"] = *fldSlug
		case "price":
			body["price"] = *fldPrice
		case "interval":
			body["interval"] = *fldInterval
		case "active":
			body["active"] = *fldActive
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPost, "/plans", body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPlansUpdate(args []string) int {
	id, rest, ok := takeID("plans update", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("plans update")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldName := fs.String("name", "", "name (string)")
	fldSlug := fs.String("slug", "", "slug (string)")
	fldPrice := fs.String("price", "", "price (decimal)")
	fldInterval := fs.String("interval", "", "interval (enum) [month|year]")
	fldActive := fs.Bool("active", false, "active (bool)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "name":
			body["name"] = *fldName
		case "slug":
			body["slug"] = *fldSlug
		case "price":
			body["price"] = *fldPrice
		case "interval":
			body["interval"] = *fldInterval
		case "active":
			body["active"] = *fldActive
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPut, "/plans/"+id, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPlansPatch(args []string) int {
	id, rest, ok := takeID("plans patch", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("plans patch")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldName := fs.String("name", "", "name (string)")
	fldSlug := fs.String("slug", "", "slug (string)")
	fldPrice := fs.String("price", "", "price (decimal)")
	fldInterval := fs.String("interval", "", "interval (enum) [month|year]")
	fldActive := fs.Bool("active", false, "active (bool)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "name":
			body["name"] = *fldName
		case "slug":
			body["slug"] = *fldSlug
		case "price":
			body["price"] = *fldPrice
		case "interval":
			body["interval"] = *fldInterval
		case "active":
			body["active"] = *fldActive
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPatch, "/plans/"+id, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPlansDelete(args []string) int {
	id, rest, ok := takeID("plans delete", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("plans delete")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, "/plans/"+id, nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %s\n", id)
	return 0
}

// runPlansBatchCreate sends a --json array through the atomic _batch route.
func runPlansBatchCreate(args []string) int {
	fs := newFlagSet("plans batch-create")
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, http.MethodPost, "/plans/_batch", map[string]any{"items": items}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runPlansBatchUpdate sends a --json array through the atomic _batch route.
func runPlansBatchUpdate(args []string) int {
	fs := newFlagSet("plans batch-update")
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, http.MethodPatch, "/plans/_batch", map[string]any{"items": items}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runPlansBatchDelete deletes the positional ids in one transaction.
func runPlansBatchDelete(args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet("plans batch-delete")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	if len(ids) == 0 {
		fmt.Println("usage: " + binaryName + " plans batch-delete <id> [id...]")
		return 2
	}
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, http.MethodDelete, "/plans/_batch", map[string]any{"ids": ids}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runPlansWatch streams the live event feed until interrupted; each event is
// one JSON line on stdout.
func runPlansWatch(args []string) int {
	fs := newFlagSet("plans watch")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	ctx, stop := signal.NotifyContext(g.ctx, os.Interrupt)
	defer stop()
	err := g.client.WatchPlans(ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%q,\"data\":%s}\n", event, data)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}
