package main

import (
	"net/http"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func paymentsCommands() []command {
	return []command{
		{name: "payments", summary: "manage payments (run for subcommands)", run: func(args []string) int { return groupUsage("payments", args) }},
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

// Verb wrappers: each binds this entity's command names and pre-escaped
// base path "/payments" to the shared verb bodies in verbs.go.

// paymentsListFilters is the filter-flag table behind `payments list`: one entry
// per flag, in help order, each bound to the query param it sets.
var paymentsListFilters = []filterFlag{
	{flag: "invoice-id", param: "invoice_id", help: "filter: invoice_id equals (comma list = IN)"},
	{flag: "invoice-id-gte", param: "invoice_id_gte", help: "filter: invoice_id greater than or equal"},
	{flag: "invoice-id-lte", param: "invoice_id_lte", help: "filter: invoice_id less than or equal"},
	{flag: "invoice-id-gt", param: "invoice_id_gt", help: "filter: invoice_id greater than"},
	{flag: "invoice-id-lt", param: "invoice_id_lt", help: "filter: invoice_id less than"},
	{flag: "invoice-id-ne", param: "invoice_id_ne", help: "filter: invoice_id not equal"},
	{flag: "customer-id", param: "customer_id", help: "filter: customer_id equals (comma list = IN)"},
	{flag: "customer-id-gte", param: "customer_id_gte", help: "filter: customer_id greater than or equal"},
	{flag: "customer-id-lte", param: "customer_id_lte", help: "filter: customer_id less than or equal"},
	{flag: "customer-id-gt", param: "customer_id_gt", help: "filter: customer_id greater than"},
	{flag: "customer-id-lt", param: "customer_id_lt", help: "filter: customer_id less than"},
	{flag: "customer-id-ne", param: "customer_id_ne", help: "filter: customer_id not equal"},
	{flag: "amount", param: "amount", help: "filter: amount equals (comma list = IN)"},
	{flag: "amount-gte", param: "amount_gte", help: "filter: amount greater than or equal"},
	{flag: "amount-lte", param: "amount_lte", help: "filter: amount less than or equal"},
	{flag: "amount-gt", param: "amount_gt", help: "filter: amount greater than"},
	{flag: "amount-lt", param: "amount_lt", help: "filter: amount less than"},
	{flag: "amount-ne", param: "amount_ne", help: "filter: amount not equal"},
	{flag: "method", param: "method", help: "filter: method equals (comma list = IN) [card|ach|wire]"},
	{flag: "method-gte", param: "method_gte", help: "filter: method greater than or equal"},
	{flag: "method-lte", param: "method_lte", help: "filter: method less than or equal"},
	{flag: "method-gt", param: "method_gt", help: "filter: method greater than"},
	{flag: "method-lt", param: "method_lt", help: "filter: method less than"},
	{flag: "method-like", param: "method_like", help: "filter: method contains"},
	{flag: "method-ne", param: "method_ne", help: "filter: method not equal"},
	{flag: "status", param: "status", help: "filter: status equals (comma list = IN) [succeeded|failed|refunded]"},
	{flag: "status-gte", param: "status_gte", help: "filter: status greater than or equal"},
	{flag: "status-lte", param: "status_lte", help: "filter: status less than or equal"},
	{flag: "status-gt", param: "status_gt", help: "filter: status greater than"},
	{flag: "status-lt", param: "status_lt", help: "filter: status less than"},
	{flag: "status-like", param: "status_like", help: "filter: status contains"},
	{flag: "status-ne", param: "status_ne", help: "filter: status not equal"},
}

// Table columns for `payments list -o table`: paymentsListHeaders are the display
// titles, paymentsListKeys the JSON wire keys each column reads.
var (
	paymentsListHeaders = []string{"id", "invoice_id", "customer_id", "amount", "method", "status"}
	paymentsListKeys    = []string{"id", "invoiceId", "customerId", "amount", "method", "status"}
)

func runPaymentsList(args []string) int {
	return runListVerb("payments list", "/payments", paymentsListFilters, paymentsListHeaders, paymentsListKeys, args)
}

func runPaymentsGet(args []string) int {
	return runGetVerb("payments get", "/payments", args)
}

// paymentsMutationFields is the field-flag table behind `payments create/update/patch`:
// one entry per writable field, each bound to the JSON wire key it sets.
var paymentsMutationFields = []mutationField{
	{flag: "invoice-id", wire: "invoiceId", kind: fieldString, usage: "invoice_id (relation)"},
	{flag: "customer-id", wire: "customerId", kind: fieldString, usage: "customer_id (relation)"},
	{flag: "amount", wire: "amount", kind: fieldString, usage: "amount (decimal)"},
	{flag: "method", wire: "method", kind: fieldString, usage: "method (enum) [card|ach|wire]"},
	{flag: "status", wire: "status", kind: fieldString, usage: "status (enum) [succeeded|failed|refunded]"},
}

func runPaymentsCreate(args []string) int {
	return runCreateVerb("payments create", "/payments", paymentsMutationFields, args)
}

func runPaymentsUpdate(args []string) int {
	return runUpdateVerb("payments update", "/payments", paymentsMutationFields, args)
}

func runPaymentsPatch(args []string) int {
	return runPatchVerb("payments patch", "/payments", paymentsMutationFields, args)
}

func runPaymentsDelete(args []string) int {
	return runDeleteVerb("payments delete", "/payments", args)
}

func runPaymentsBatchCreate(args []string) int {
	return runBatchJSONVerb("payments batch-create", "/payments", http.MethodPost, args)
}

func runPaymentsBatchUpdate(args []string) int {
	return runBatchJSONVerb("payments batch-update", "/payments", http.MethodPatch, args)
}

func runPaymentsBatchDelete(args []string) int {
	return runBatchDeleteVerb("payments batch-delete", "/payments", args)
}

func runPaymentsWatch(args []string) int {
	return runWatchVerb("payments watch", (*client.Client).WatchPayments, args)
}
