package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func invoicesCommands() []command {
	return []command{
		{name: "invoices list", summary: "list invoices (filters, sort, pagination)", run: runInvoicesList},
		{name: "invoices get", summary: "fetch one record by id", run: runInvoicesGet},
		{name: "invoices create", summary: "create a record from field flags or --json", run: runInvoicesCreate},
		{name: "invoices update", summary: "update fields on a record (PUT)", run: runInvoicesUpdate},
		{name: "invoices patch", summary: "patch fields on a record (PATCH)", run: runInvoicesPatch},
		{name: "invoices delete", summary: "delete a record by id", run: runInvoicesDelete},
		{name: "invoices batch-create", summary: "create up to 100 records atomically (--json array)", run: runInvoicesBatchCreate},
		{name: "invoices batch-update", summary: "patch up to 100 records atomically (--json array)", run: runInvoicesBatchUpdate},
		{name: "invoices batch-delete", summary: "delete ids atomically (positional ids)", run: runInvoicesBatchDelete},
		{name: "invoices watch", summary: "stream live create/update/delete events (SSE)", run: runInvoicesWatch},
	}
}

func runInvoicesList(args []string) int {
	fs := newFlagSet("invoices list")
	sortF := fs.String("sort", "", "sort field(s), comma-separated, - prefix for desc")
	page := fs.String("page", "", "page number (offset pagination)")
	limit := fs.String("limit", "", "page size")
	cursor := fs.String("cursor", "", "keyset cursor (from a prior response)")
	include := fs.String("include", "", "relations to eager-load (comma, dots for nesting)")
	fieldsF := fs.String("fields", "", "sparse field projection (comma-separated)")
	outF := fs.String("o", "json", "output format: json|table")
	var params paramFlags
	fs.Var(&params, "param", "extra query param key=value (repeatable)")
	fltCustomerId := fs.String("customer-id", "", "filter: customer_id equals (comma list = IN)")
	fltNumber := fs.String("number", "", "filter: number equals (comma list = IN)")
	fltNumberLike := fs.String("number-like", "", "filter: number contains")
	fltAmount := fs.String("amount", "", "filter: amount equals (comma list = IN)")
	fltAmountGT := fs.String("amount-gt", "", "filter: amount gt")
	fltAmountGTE := fs.String("amount-gte", "", "filter: amount gte")
	fltAmountLT := fs.String("amount-lt", "", "filter: amount lt")
	fltAmountLTE := fs.String("amount-lte", "", "filter: amount lte")
	fltStatus := fs.String("status", "", "filter: status equals (comma list = IN) [draft|open|paid|past_due|void]")
	fltIssuedOn := fs.String("issued-on", "", "filter: issued_on equals (comma list = IN)")
	fltIssuedOnGT := fs.String("issued-on-gt", "", "filter: issued_on gt")
	fltIssuedOnGTE := fs.String("issued-on-gte", "", "filter: issued_on gte")
	fltIssuedOnLT := fs.String("issued-on-lt", "", "filter: issued_on lt")
	fltIssuedOnLTE := fs.String("issued-on-lte", "", "filter: issued_on lte")
	fltDueOn := fs.String("due-on", "", "filter: due_on equals (comma list = IN)")
	fltDueOnGT := fs.String("due-on-gt", "", "filter: due_on gt")
	fltDueOnGTE := fs.String("due-on-gte", "", "filter: due_on gte")
	fltDueOnLT := fs.String("due-on-lt", "", "filter: due_on lt")
	fltDueOnLTE := fs.String("due-on-lte", "", "filter: due_on lte")
	fltPaidOn := fs.String("paid-on", "", "filter: paid_on equals (comma list = IN)")
	fltPaidOnGT := fs.String("paid-on-gt", "", "filter: paid_on gt")
	fltPaidOnGTE := fs.String("paid-on-gte", "", "filter: paid_on gte")
	fltPaidOnLT := fs.String("paid-on-lt", "", "filter: paid_on lt")
	fltPaidOnLTE := fs.String("paid-on-lte", "", "filter: paid_on lte")
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
	set("customer_id", *fltCustomerId)
	set("number", *fltNumber)
	set("number_like", *fltNumberLike)
	set("amount", *fltAmount)
	set("amount_gt", *fltAmountGT)
	set("amount_gte", *fltAmountGTE)
	set("amount_lt", *fltAmountLT)
	set("amount_lte", *fltAmountLTE)
	set("status", *fltStatus)
	set("issued_on", *fltIssuedOn)
	set("issued_on_gt", *fltIssuedOnGT)
	set("issued_on_gte", *fltIssuedOnGTE)
	set("issued_on_lt", *fltIssuedOnLT)
	set("issued_on_lte", *fltIssuedOnLTE)
	set("due_on", *fltDueOn)
	set("due_on_gt", *fltDueOnGT)
	set("due_on_gte", *fltDueOnGTE)
	set("due_on_lt", *fltDueOnLT)
	set("due_on_lte", *fltDueOnLTE)
	set("paid_on", *fltPaidOn)
	set("paid_on_gt", *fltPaidOnGT)
	set("paid_on_gte", *fltPaidOnGTE)
	set("paid_on_lt", *fltPaidOnLT)
	set("paid_on_lte", *fltPaidOnLTE)
	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
	path := "/invoices"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
		printListTable([]string{"id", "customer_id", "number", "amount", "status", "issued_on", "due_on", "paid_on"}, []string{"id", "customerId", "number", "amount", "status", "issuedOn", "dueOn", "paidOn"}, resp.Data)
		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows — next cursor: %s\n", len(resp.Data), resp.Cursor)
		} else {
			fmt.Printf("page %d/%d — %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

func runInvoicesGet(args []string) int {
	id, rest, ok := takeID("invoices get", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("invoices get")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, "/invoices/"+id, nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runInvoicesCreate(args []string) int {
	fs := newFlagSet("invoices create")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldNumber := fs.String("number", "", "number (string)")
	fldAmount := fs.String("amount", "", "amount (decimal)")
	fldStatus := fs.String("status", "", "status (enum) [draft|open|paid|past_due|void]")
	fldIssuedOn := fs.String("issued-on", "", "issued_on (date)")
	fldDueOn := fs.String("due-on", "", "due_on (date)")
	fldPaidOn := fs.String("paid-on", "", "paid_on (date)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "number":
			body["number"] = *fldNumber
		case "amount":
			body["amount"] = *fldAmount
		case "status":
			body["status"] = *fldStatus
		case "issued-on":
			body["issuedOn"] = *fldIssuedOn
		case "due-on":
			body["dueOn"] = *fldDueOn
		case "paid-on":
			body["paidOn"] = *fldPaidOn
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPost, "/invoices", body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runInvoicesUpdate(args []string) int {
	id, rest, ok := takeID("invoices update", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("invoices update")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldNumber := fs.String("number", "", "number (string)")
	fldAmount := fs.String("amount", "", "amount (decimal)")
	fldStatus := fs.String("status", "", "status (enum) [draft|open|paid|past_due|void]")
	fldIssuedOn := fs.String("issued-on", "", "issued_on (date)")
	fldDueOn := fs.String("due-on", "", "due_on (date)")
	fldPaidOn := fs.String("paid-on", "", "paid_on (date)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "number":
			body["number"] = *fldNumber
		case "amount":
			body["amount"] = *fldAmount
		case "status":
			body["status"] = *fldStatus
		case "issued-on":
			body["issuedOn"] = *fldIssuedOn
		case "due-on":
			body["dueOn"] = *fldDueOn
		case "paid-on":
			body["paidOn"] = *fldPaidOn
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPut, "/invoices/"+id, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runInvoicesPatch(args []string) int {
	id, rest, ok := takeID("invoices patch", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("invoices patch")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldNumber := fs.String("number", "", "number (string)")
	fldAmount := fs.String("amount", "", "amount (decimal)")
	fldStatus := fs.String("status", "", "status (enum) [draft|open|paid|past_due|void]")
	fldIssuedOn := fs.String("issued-on", "", "issued_on (date)")
	fldDueOn := fs.String("due-on", "", "due_on (date)")
	fldPaidOn := fs.String("paid-on", "", "paid_on (date)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "number":
			body["number"] = *fldNumber
		case "amount":
			body["amount"] = *fldAmount
		case "status":
			body["status"] = *fldStatus
		case "issued-on":
			body["issuedOn"] = *fldIssuedOn
		case "due-on":
			body["dueOn"] = *fldDueOn
		case "paid-on":
			body["paidOn"] = *fldPaidOn
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPatch, "/invoices/"+id, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runInvoicesDelete(args []string) int {
	id, rest, ok := takeID("invoices delete", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("invoices delete")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, "/invoices/"+id, nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %s\n", id)
	return 0
}

// runInvoicesBatchCreate sends a --json array through the atomic _batch route.
func runInvoicesBatchCreate(args []string) int {
	fs := newFlagSet("invoices batch-create")
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
	if err := g.client.Do(g.ctx, http.MethodPost, "/invoices/_batch", map[string]any{"items": items}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runInvoicesBatchUpdate sends a --json array through the atomic _batch route.
func runInvoicesBatchUpdate(args []string) int {
	fs := newFlagSet("invoices batch-update")
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
	if err := g.client.Do(g.ctx, http.MethodPatch, "/invoices/_batch", map[string]any{"items": items}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runInvoicesBatchDelete deletes the positional ids in one transaction.
func runInvoicesBatchDelete(args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet("invoices batch-delete")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	if len(ids) == 0 {
		fmt.Println("usage: " + binaryName + " invoices batch-delete <id> [id...]")
		return 2
	}
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, http.MethodDelete, "/invoices/_batch", map[string]any{"ids": ids}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runInvoicesWatch streams the live event feed until interrupted; each event is
// one JSON line on stdout.
func runInvoicesWatch(args []string) int {
	fs := newFlagSet("invoices watch")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	ctx, stop := signal.NotifyContext(g.ctx, os.Interrupt)
	defer stop()
	err := g.client.WatchInvoices(ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%q,\"data\":%s}\n", event, data)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}
