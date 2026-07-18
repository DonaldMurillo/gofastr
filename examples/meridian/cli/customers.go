package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
)

func customersCommands() []command {
	return []command{
		{name: "customers list", summary: "list customers (filters, sort, pagination)", run: runCustomersList},
		{name: "customers get", summary: "fetch one record by id", run: runCustomersGet},
		{name: "customers create", summary: "create a record from field flags or --json", run: runCustomersCreate},
		{name: "customers update", summary: "update fields on a record (PUT)", run: runCustomersUpdate},
		{name: "customers patch", summary: "patch fields on a record (PATCH)", run: runCustomersPatch},
		{name: "customers delete", summary: "delete a record by id", run: runCustomersDelete},
		{name: "customers batch-create", summary: "create up to 100 records atomically (--json array)", run: runCustomersBatchCreate},
		{name: "customers batch-update", summary: "patch up to 100 records atomically (--json array)", run: runCustomersBatchUpdate},
		{name: "customers batch-delete", summary: "delete ids atomically (positional ids)", run: runCustomersBatchDelete},
		{name: "customers watch", summary: "stream live create/update/delete events (SSE)", run: runCustomersWatch},
	}
}

func runCustomersList(args []string) int {
	fs := newFlagSet("customers list")
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
	fltEmail := fs.String("email", "", "filter: email equals (comma list = IN)")
	fltEmailLike := fs.String("email-like", "", "filter: email contains")
	fltCompany := fs.String("company", "", "filter: company equals (comma list = IN)")
	fltCompanyLike := fs.String("company-like", "", "filter: company contains")
	fltStatus := fs.String("status", "", "filter: status equals (comma list = IN) [trialing|active|past_due|canceled]")
	fltMrr := fs.String("mrr", "", "filter: mrr equals (comma list = IN)")
	fltMrrGT := fs.String("mrr-gt", "", "filter: mrr gt")
	fltMrrGTE := fs.String("mrr-gte", "", "filter: mrr gte")
	fltMrrLT := fs.String("mrr-lt", "", "filter: mrr lt")
	fltMrrLTE := fs.String("mrr-lte", "", "filter: mrr lte")
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
	set("email", *fltEmail)
	set("email_like", *fltEmailLike)
	set("company", *fltCompany)
	set("company_like", *fltCompanyLike)
	set("status", *fltStatus)
	set("mrr", *fltMrr)
	set("mrr_gt", *fltMrrGT)
	set("mrr_gte", *fltMrrGTE)
	set("mrr_lt", *fltMrrLT)
	set("mrr_lte", *fltMrrLTE)
	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
	path := "/customers"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
		printListTable([]string{"id", "name", "email", "company", "status", "mrr"}, []string{"id", "name", "email", "company", "status", "mrr"}, resp.Data)
		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows — next cursor: %s\n", len(resp.Data), resp.Cursor)
		} else {
			fmt.Printf("page %d/%d — %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

func runCustomersGet(args []string) int {
	id, rest, ok := takeID("customers get", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("customers get")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, "/customers/"+url.PathEscape(id), nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runCustomersCreate(args []string) int {
	fs := newFlagSet("customers create")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldName := fs.String("name", "", "name (string)")
	fldEmail := fs.String("email", "", "email (string)")
	fldCompany := fs.String("company", "", "company (string)")
	fldStatus := fs.String("status", "", "status (enum) [trialing|active|past_due|canceled]")
	fldMrr := fs.String("mrr", "", "mrr (decimal)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "name":
			body["name"] = *fldName
		case "email":
			body["email"] = *fldEmail
		case "company":
			body["company"] = *fldCompany
		case "status":
			body["status"] = *fldStatus
		case "mrr":
			body["mrr"] = *fldMrr
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPost, "/customers", body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runCustomersUpdate(args []string) int {
	id, rest, ok := takeID("customers update", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("customers update")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldName := fs.String("name", "", "name (string)")
	fldEmail := fs.String("email", "", "email (string)")
	fldCompany := fs.String("company", "", "company (string)")
	fldStatus := fs.String("status", "", "status (enum) [trialing|active|past_due|canceled]")
	fldMrr := fs.String("mrr", "", "mrr (decimal)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "name":
			body["name"] = *fldName
		case "email":
			body["email"] = *fldEmail
		case "company":
			body["company"] = *fldCompany
		case "status":
			body["status"] = *fldStatus
		case "mrr":
			body["mrr"] = *fldMrr
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPut, "/customers/"+url.PathEscape(id), body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runCustomersPatch(args []string) int {
	id, rest, ok := takeID("customers patch", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("customers patch")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldName := fs.String("name", "", "name (string)")
	fldEmail := fs.String("email", "", "email (string)")
	fldCompany := fs.String("company", "", "company (string)")
	fldStatus := fs.String("status", "", "status (enum) [trialing|active|past_due|canceled]")
	fldMrr := fs.String("mrr", "", "mrr (decimal)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "name":
			body["name"] = *fldName
		case "email":
			body["email"] = *fldEmail
		case "company":
			body["company"] = *fldCompany
		case "status":
			body["status"] = *fldStatus
		case "mrr":
			body["mrr"] = *fldMrr
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPatch, "/customers/"+url.PathEscape(id), body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runCustomersDelete(args []string) int {
	id, rest, ok := takeID("customers delete", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("customers delete")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, "/customers/"+url.PathEscape(id), nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %s\n", id)
	return 0
}

// runCustomersBatchCreate sends a --json array through the atomic _batch route. A rolled-
// back batch prints its {committed, results[]} envelope and exits 1.
func runCustomersBatchCreate(args []string) int {
	fs := newFlagSet("customers batch-create")
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	resp, code := doBatch(g, http.MethodPost, "/customers/_batch", map[string]any{"items": items})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runCustomersBatchUpdate sends a --json array through the atomic _batch route. A rolled-
// back batch prints its {committed, results[]} envelope and exits 1.
func runCustomersBatchUpdate(args []string) int {
	fs := newFlagSet("customers batch-update")
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	resp, code := doBatch(g, http.MethodPatch, "/customers/_batch", map[string]any{"items": items})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runCustomersBatchDelete deletes the positional ids in one transaction. Ids may
// appear before or after flags — flag.Parse stops at the first positional,
// so the trailing ones are collected from fs.Args().
func runCustomersBatchDelete(args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet("customers batch-delete")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	for _, id := range fs.Args() {
		if id != "" && id[0] == '-' {
			fmt.Println(binaryName + " customers batch-delete: flags must precede trailing ids (got " + id + " after an id)")
			return 2
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fmt.Println("usage: " + binaryName + " customers batch-delete <id> [id...]")
		return 2
	}
	resp, code := doBatch(g, http.MethodDelete, "/customers/_batch", map[string]any{"ids": ids})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runCustomersWatch streams the live event feed until interrupted; each event is
// one JSON line on stdout.
func runCustomersWatch(args []string) int {
	fs := newFlagSet("customers watch")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	ctx, stop := signal.NotifyContext(g.ctx, os.Interrupt)
	defer stop()
	err := g.client.WatchCustomers(ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%q,\"data\":%s}\n", event, data)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}
