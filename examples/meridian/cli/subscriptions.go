package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func subscriptionsCommands() []command {
	return []command{
		{name: "subscriptions list", summary: "list subscriptions (filters, sort, pagination)", run: runSubscriptionsList},
		{name: "subscriptions get", summary: "fetch one record by id", run: runSubscriptionsGet},
		{name: "subscriptions create", summary: "create a record from field flags or --json", run: runSubscriptionsCreate},
		{name: "subscriptions update", summary: "update fields on a record (PUT)", run: runSubscriptionsUpdate},
		{name: "subscriptions patch", summary: "patch fields on a record (PATCH)", run: runSubscriptionsPatch},
		{name: "subscriptions delete", summary: "delete a record by id", run: runSubscriptionsDelete},
		{name: "subscriptions batch-create", summary: "create up to 100 records atomically (--json array)", run: runSubscriptionsBatchCreate},
		{name: "subscriptions batch-update", summary: "patch up to 100 records atomically (--json array)", run: runSubscriptionsBatchUpdate},
		{name: "subscriptions batch-delete", summary: "delete ids atomically (positional ids)", run: runSubscriptionsBatchDelete},
		{name: "subscriptions watch", summary: "stream live create/update/delete events (SSE)", run: runSubscriptionsWatch},
	}
}

func runSubscriptionsList(args []string) int {
	fs := newFlagSet("subscriptions list")
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
	fltPlanId := fs.String("plan-id", "", "filter: plan_id equals (comma list = IN)")
	fltStatus := fs.String("status", "", "filter: status equals (comma list = IN) [trialing|active|past_due|canceled]")
	fltMrr := fs.String("mrr", "", "filter: mrr equals (comma list = IN)")
	fltMrrGT := fs.String("mrr-gt", "", "filter: mrr gt")
	fltMrrGTE := fs.String("mrr-gte", "", "filter: mrr gte")
	fltMrrLT := fs.String("mrr-lt", "", "filter: mrr lt")
	fltMrrLTE := fs.String("mrr-lte", "", "filter: mrr lte")
	fltStartedOn := fs.String("started-on", "", "filter: started_on equals (comma list = IN)")
	fltStartedOnGT := fs.String("started-on-gt", "", "filter: started_on gt")
	fltStartedOnGTE := fs.String("started-on-gte", "", "filter: started_on gte")
	fltStartedOnLT := fs.String("started-on-lt", "", "filter: started_on lt")
	fltStartedOnLTE := fs.String("started-on-lte", "", "filter: started_on lte")
	fltRenewsOn := fs.String("renews-on", "", "filter: renews_on equals (comma list = IN)")
	fltRenewsOnGT := fs.String("renews-on-gt", "", "filter: renews_on gt")
	fltRenewsOnGTE := fs.String("renews-on-gte", "", "filter: renews_on gte")
	fltRenewsOnLT := fs.String("renews-on-lt", "", "filter: renews_on lt")
	fltRenewsOnLTE := fs.String("renews-on-lte", "", "filter: renews_on lte")
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
	set("plan_id", *fltPlanId)
	set("status", *fltStatus)
	set("mrr", *fltMrr)
	set("mrr_gt", *fltMrrGT)
	set("mrr_gte", *fltMrrGTE)
	set("mrr_lt", *fltMrrLT)
	set("mrr_lte", *fltMrrLTE)
	set("started_on", *fltStartedOn)
	set("started_on_gt", *fltStartedOnGT)
	set("started_on_gte", *fltStartedOnGTE)
	set("started_on_lt", *fltStartedOnLT)
	set("started_on_lte", *fltStartedOnLTE)
	set("renews_on", *fltRenewsOn)
	set("renews_on_gt", *fltRenewsOnGT)
	set("renews_on_gte", *fltRenewsOnGTE)
	set("renews_on_lt", *fltRenewsOnLT)
	set("renews_on_lte", *fltRenewsOnLTE)
	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
	path := "/subscriptions"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
		printListTable([]string{"id", "customer_id", "plan_id", "status", "mrr", "started_on", "renews_on"}, []string{"id", "customerId", "planId", "status", "mrr", "startedOn", "renewsOn"}, resp.Data)
		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows — next cursor: %s\n", len(resp.Data), resp.Cursor)
		} else {
			fmt.Printf("page %d/%d — %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

func runSubscriptionsGet(args []string) int {
	id, rest, ok := takeID("subscriptions get", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("subscriptions get")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, "/subscriptions/"+id, nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runSubscriptionsCreate(args []string) int {
	fs := newFlagSet("subscriptions create")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldPlanId := fs.String("plan-id", "", "plan_id (relation)")
	fldStatus := fs.String("status", "", "status (enum) [trialing|active|past_due|canceled]")
	fldMrr := fs.String("mrr", "", "mrr (decimal)")
	fldStartedOn := fs.String("started-on", "", "started_on (date)")
	fldRenewsOn := fs.String("renews-on", "", "renews_on (date)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "plan-id":
			body["planId"] = *fldPlanId
		case "status":
			body["status"] = *fldStatus
		case "mrr":
			body["mrr"] = *fldMrr
		case "started-on":
			body["startedOn"] = *fldStartedOn
		case "renews-on":
			body["renewsOn"] = *fldRenewsOn
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPost, "/subscriptions", body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runSubscriptionsUpdate(args []string) int {
	id, rest, ok := takeID("subscriptions update", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("subscriptions update")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldPlanId := fs.String("plan-id", "", "plan_id (relation)")
	fldStatus := fs.String("status", "", "status (enum) [trialing|active|past_due|canceled]")
	fldMrr := fs.String("mrr", "", "mrr (decimal)")
	fldStartedOn := fs.String("started-on", "", "started_on (date)")
	fldRenewsOn := fs.String("renews-on", "", "renews_on (date)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "plan-id":
			body["planId"] = *fldPlanId
		case "status":
			body["status"] = *fldStatus
		case "mrr":
			body["mrr"] = *fldMrr
		case "started-on":
			body["startedOn"] = *fldStartedOn
		case "renews-on":
			body["renewsOn"] = *fldRenewsOn
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPut, "/subscriptions/"+id, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runSubscriptionsPatch(args []string) int {
	id, rest, ok := takeID("subscriptions patch", args)
	if !ok {
		return 2
	}
	args = rest
	fs := newFlagSet("subscriptions patch")
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	fldCustomerId := fs.String("customer-id", "", "customer_id (relation)")
	fldPlanId := fs.String("plan-id", "", "plan_id (relation)")
	fldStatus := fs.String("status", "", "status (enum) [trialing|active|past_due|canceled]")
	fldMrr := fs.String("mrr", "", "mrr (decimal)")
	fldStartedOn := fs.String("started-on", "", "started_on (date)")
	fldRenewsOn := fs.String("renews-on", "", "renews_on (date)")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
		case "customer-id":
			body["customerId"] = *fldCustomerId
		case "plan-id":
			body["planId"] = *fldPlanId
		case "status":
			body["status"] = *fldStatus
		case "mrr":
			body["mrr"] = *fldMrr
		case "started-on":
			body["startedOn"] = *fldStartedOn
		case "renews-on":
			body["renewsOn"] = *fldRenewsOn
		}
		return nil
	})
	if code != 0 {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodPatch, "/subscriptions/"+id, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

func runSubscriptionsDelete(args []string) int {
	id, rest, ok := takeID("subscriptions delete", args)
	if !ok {
		return 2
	}
	fs := newFlagSet("subscriptions delete")
	g, code := parseGlobals(fs, rest)
	if code != 0 {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, "/subscriptions/"+id, nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %s\n", id)
	return 0
}

// runSubscriptionsBatchCreate sends a --json array through the atomic _batch route.
func runSubscriptionsBatchCreate(args []string) int {
	fs := newFlagSet("subscriptions batch-create")
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
	if err := g.client.Do(g.ctx, http.MethodPost, "/subscriptions/_batch", map[string]any{"items": items}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runSubscriptionsBatchUpdate sends a --json array through the atomic _batch route.
func runSubscriptionsBatchUpdate(args []string) int {
	fs := newFlagSet("subscriptions batch-update")
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
	if err := g.client.Do(g.ctx, http.MethodPatch, "/subscriptions/_batch", map[string]any{"items": items}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runSubscriptionsBatchDelete deletes the positional ids in one transaction.
func runSubscriptionsBatchDelete(args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet("subscriptions batch-delete")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	if len(ids) == 0 {
		fmt.Println("usage: " + binaryName + " subscriptions batch-delete <id> [id...]")
		return 2
	}
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, http.MethodDelete, "/subscriptions/_batch", map[string]any{"ids": ids}, &resp); err != nil {
		return apiFail(err)
	}
	return printBatch(resp)
}

// runSubscriptionsWatch streams the live event feed until interrupted; each event is
// one JSON line on stdout.
func runSubscriptionsWatch(args []string) int {
	fs := newFlagSet("subscriptions watch")
	g, code := parseGlobals(fs, args)
	if code != 0 {
		return code
	}
	ctx, stop := signal.NotifyContext(g.ctx, os.Interrupt)
	defer stop()
	err := g.client.WatchSubscriptions(ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%q,\"data\":%s}\n", event, data)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}
