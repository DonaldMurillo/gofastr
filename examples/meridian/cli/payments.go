package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
)

func paymentsCommands() []command {
	return []command{
		{name: "payments list", summary: "list payments (filters, sort, pagination)", run: runPaymentsList},
		{name: "payments get", summary: "fetch one record by id", run: runPaymentsGet},
		{name: "payments create", summary: "create a record from field flags or --json", run: runPaymentsCreate},
		{name: "payments update", summary: "update fields on a record (PUT)", run: runPaymentsUpdate},
		{name: "payments patch", summary: "patch fields on a record (PATCH)", run: runPaymentsPatch},
		{name: "payments delete", summary: "delete a record by id", run: runPaymentsDelete},
		{name: "payments batch-create", summary: "create up to 100 records atomically (--json array)", run: runPaymentsBatchCreate},
		{name: "payments batch-update", summary: "patch up to 100 records atomically (--json array)", run: runPaymentsBatchUpdate},
		{name: "payments batch-delete", summary: "delete ids atomically (positional ids)", run: runPaymentsBatchDelete},
		{name: "payments watch", summary: "stream live create/update/delete events (SSE)", run: runPaymentsWatch},
	}
}

func runPaymentsList(args []string) int {
	fs := newFlagSet("payments list")
	sortF := fs.String("sort", "", "sort field(s), comma-separated, - prefix for desc")
	page := fs.String("page", "", "page number (offset pagination)")
	limit := fs.String("limit", "", "page size")
	cursor := fs.String("cursor", "", "keyset cursor (from a prior response)")
	include := fs.String("include", "", "relations to eager-load (comma, dots for nesting)")
	fieldsF := fs.String("fields", "", "sparse field projection (comma-separated)")
	outF := fs.String("o", "json", "output format: json|table")
	var params paramFlags
	fs.Var(&params, "param", "extra query param key=value (repeatable)")
	fltInvoiceId := fs.String("invoice-id", "", "filter: invoice_id equals (comma list = IN)")
	fltCustomerId := fs.String("customer-id", "", "filter: customer_id equals (comma list = IN)")
	fltAmount := fs.String("amount", "", "filter: amount equals (comma list = IN)")
	fltAmountGT := fs.String("amount-gt", "", "filter: amount gt")
	fltAmountGTE := fs.String("amount-gte", "", "filter: amount gte")
	fltAmountLT := fs.String("amount-lt", "", "filter: amount lt")
	fltAmountLTE := fs.String("amount-lte", "", "filter: amount lte")
	fltMethod := fs.String("method", "", "filter: method equals (comma list = IN) [card|ach|wire]")
	fltStatus := fs.String("status", "", "filter: status equals (comma list = IN) [succeeded|failed|refunded]")
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
	set("invoice_id", *fltInvoiceId)
	set("customer_id", *fltCustomerId)
	set("amount", *fltAmount)
	set("amount_gt", *fltAmountGT)
	set("amount_gte", *fltAmountGTE)
	set("amount_lt", *fltAmountLT)
	set("amount_lte", *fltAmountLTE)
	set("method", *fltMethod)
	set("status", *fltStatus)
	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
	path := "/payments"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
		printListTable([]string{"id", "invoice_id", "customer_id", "amount", "method", "status"}, []string{"id", "invoiceId", "customerId", "amount", "method", "status"}, resp.Data)
		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows — next cursor: %s\n", len(resp.Data), resp.Cursor)
		} else {
			fmt.Printf("page %d/%d — %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

func runPaymentsGet(args []string) int {
	id, rest, ok := takeID("payments get", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("payments get")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, "/payments/"+url.PathEscape(id), nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPaymentsCreate(args []string) int {
	fs := newFlagSet("payments create")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldInvoiceId := fs.String("invoice-id", "", "invoice_id (relation)")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldAmount := fs.String("amount", "", "amount (decimal)")
	fldMethod := fs.String("method", "", "method (enum) [card|ach|wire]")
	fldStatus := fs.String("status", "", "status (enum) [succeeded|failed|refunded]")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "invoice-id":
			body["invoiceId"] = *fldInvoiceId
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "amount":
			body["amount"] = *fldAmount
		case "method":
			body["method"] = *fldMethod
		case "status":
			body["status"] = *fldStatus
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPost, "/payments", body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPaymentsUpdate(args []string) int {
	id, rest, ok := takeID("payments update", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("payments update")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldInvoiceId := fs.String("invoice-id", "", "invoice_id (relation)")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldAmount := fs.String("amount", "", "amount (decimal)")
	fldMethod := fs.String("method", "", "method (enum) [card|ach|wire]")
	fldStatus := fs.String("status", "", "status (enum) [succeeded|failed|refunded]")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "invoice-id":
			body["invoiceId"] = *fldInvoiceId
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "amount":
			body["amount"] = *fldAmount
		case "method":
			body["method"] = *fldMethod
		case "status":
			body["status"] = *fldStatus
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPut, "/payments/"+url.PathEscape(id), body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPaymentsPatch(args []string) int {
	id, rest, ok := takeID("payments patch", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("payments patch")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldInvoiceId := fs.String("invoice-id", "", "invoice_id (relation)")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldAmount := fs.String("amount", "", "amount (decimal)")
	fldMethod := fs.String("method", "", "method (enum) [card|ach|wire]")
	fldStatus := fs.String("status", "", "status (enum) [succeeded|failed|refunded]")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "invoice-id":
			body["invoiceId"] = *fldInvoiceId
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "amount":
			body["amount"] = *fldAmount
		case "method":
			body["method"] = *fldMethod
		case "status":
			body["status"] = *fldStatus
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPatch, "/payments/"+url.PathEscape(id), body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runPaymentsDelete(args []string) int {
	id, rest, ok := takeID("payments delete", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("payments delete")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, "/payments/"+url.PathEscape(id), nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %s\n", id)
	return 0
}

// runPaymentsBatchCreate sends a --json array through the atomic _batch route. A rolled-
// back batch prints its {committed, results[]} envelope and exits 1.
func runPaymentsBatchCreate(args []string) int {
	fs := newFlagSet("payments batch-create")
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	resp, code := doBatch(g, http.MethodPost, "/payments/_batch", map[string]any{"items": items})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runPaymentsBatchUpdate sends a --json array through the atomic _batch route. A rolled-
// back batch prints its {committed, results[]} envelope and exits 1.
func runPaymentsBatchUpdate(args []string) int {
	fs := newFlagSet("payments batch-update")
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	resp, code := doBatch(g, http.MethodPatch, "/payments/_batch", map[string]any{"items": items})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runPaymentsBatchDelete deletes the positional ids in one transaction. Ids may
// appear before or after flags — flag.Parse stops at the first positional,
// so the trailing ones are collected from fs.Args().
func runPaymentsBatchDelete(args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet("payments batch-delete")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	for _, id := range fs.Args() {
		if id != "" && id[0] == '-' {
			fmt.Println(binaryName + " payments batch-delete: flags must precede trailing ids (got " + id + " after an id)")
			return 2
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fmt.Println("usage: " + binaryName + " payments batch-delete <id> [id...]")
		return 2
	}
	resp, code := doBatch(g, http.MethodDelete, "/payments/_batch", map[string]any{"ids": ids})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

// runPaymentsWatch streams the live event feed until interrupted; each event is
// one JSON line on stdout.
func runPaymentsWatch(args []string) int {
	fs := newFlagSet("payments watch")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	ctx, stop := signal.NotifyContext(g.ctx, os.Interrupt)
	defer stop()
	err := g.client.WatchPayments(ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%q,\"data\":%s}\n", event, data)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}
