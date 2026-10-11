package main

import (
	"net/http"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func invoicesCommands() []command {
	return []command{
		{name: "invoices", summary: "manage invoices (run for subcommands)", run: func(args []string) int { return groupUsage("invoices", args) }},
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
		{name: "invoices mark_paid", summary: "move status from draft or open or past_due to paid, stamps paid_on", run: runInvoicesMarkPaid},
		{name: "invoices void", summary: "move status from draft or open or past_due to void", run: runInvoicesVoid},
	}
}

// Verb wrappers: each binds this entity's command names and pre-escaped
// base path "/invoices" to the shared verb bodies in verbs.go.

// invoicesListFilters is the filter-flag table behind `invoices list`: one entry
// per flag, in help order, each bound to the query param it sets.
var invoicesListFilters = []filterFlag{
	{flag: "q", param: "q", help: "free-text search over the declared search fields"},
	{flag: "customer-id", param: "customer_id", help: "filter: customer_id equals (comma list = IN)"},
	{flag: "customer-id-gte", param: "customer_id_gte", help: "filter: customer_id greater than or equal"},
	{flag: "customer-id-lte", param: "customer_id_lte", help: "filter: customer_id less than or equal"},
	{flag: "customer-id-gt", param: "customer_id_gt", help: "filter: customer_id greater than"},
	{flag: "customer-id-lt", param: "customer_id_lt", help: "filter: customer_id less than"},
	{flag: "customer-id-ne", param: "customer_id_ne", help: "filter: customer_id not equal"},
	{flag: "number", param: "number", help: "filter: number equals (comma list = IN)"},
	{flag: "number-gte", param: "number_gte", help: "filter: number greater than or equal"},
	{flag: "number-lte", param: "number_lte", help: "filter: number less than or equal"},
	{flag: "number-gt", param: "number_gt", help: "filter: number greater than"},
	{flag: "number-lt", param: "number_lt", help: "filter: number less than"},
	{flag: "number-like", param: "number_like", help: "filter: number contains"},
	{flag: "number-ne", param: "number_ne", help: "filter: number not equal"},
	{flag: "amount", param: "amount", help: "filter: amount equals (comma list = IN)"},
	{flag: "amount-gte", param: "amount_gte", help: "filter: amount greater than or equal"},
	{flag: "amount-lte", param: "amount_lte", help: "filter: amount less than or equal"},
	{flag: "amount-gt", param: "amount_gt", help: "filter: amount greater than"},
	{flag: "amount-lt", param: "amount_lt", help: "filter: amount less than"},
	{flag: "amount-ne", param: "amount_ne", help: "filter: amount not equal"},
	{flag: "status", param: "status", help: "filter: status equals (comma list = IN) [draft|open|paid|past_due|void]"},
	{flag: "status-gte", param: "status_gte", help: "filter: status greater than or equal"},
	{flag: "status-lte", param: "status_lte", help: "filter: status less than or equal"},
	{flag: "status-gt", param: "status_gt", help: "filter: status greater than"},
	{flag: "status-lt", param: "status_lt", help: "filter: status less than"},
	{flag: "status-like", param: "status_like", help: "filter: status contains"},
	{flag: "status-ne", param: "status_ne", help: "filter: status not equal"},
	{flag: "issued-on", param: "issued_on", help: "filter: issued_on equals (comma list = IN)"},
	{flag: "issued-on-gte", param: "issued_on_gte", help: "filter: issued_on greater than or equal"},
	{flag: "issued-on-lte", param: "issued_on_lte", help: "filter: issued_on less than or equal"},
	{flag: "issued-on-gt", param: "issued_on_gt", help: "filter: issued_on greater than"},
	{flag: "issued-on-lt", param: "issued_on_lt", help: "filter: issued_on less than"},
	{flag: "issued-on-ne", param: "issued_on_ne", help: "filter: issued_on not equal"},
	{flag: "due-on", param: "due_on", help: "filter: due_on equals (comma list = IN)"},
	{flag: "due-on-gte", param: "due_on_gte", help: "filter: due_on greater than or equal"},
	{flag: "due-on-lte", param: "due_on_lte", help: "filter: due_on less than or equal"},
	{flag: "due-on-gt", param: "due_on_gt", help: "filter: due_on greater than"},
	{flag: "due-on-lt", param: "due_on_lt", help: "filter: due_on less than"},
	{flag: "due-on-ne", param: "due_on_ne", help: "filter: due_on not equal"},
	{flag: "paid-on", param: "paid_on", help: "filter: paid_on equals (comma list = IN)"},
	{flag: "paid-on-gte", param: "paid_on_gte", help: "filter: paid_on greater than or equal"},
	{flag: "paid-on-lte", param: "paid_on_lte", help: "filter: paid_on less than or equal"},
	{flag: "paid-on-gt", param: "paid_on_gt", help: "filter: paid_on greater than"},
	{flag: "paid-on-lt", param: "paid_on_lt", help: "filter: paid_on less than"},
	{flag: "paid-on-ne", param: "paid_on_ne", help: "filter: paid_on not equal"},
}

// Table columns for `invoices list -o table`: invoicesListHeaders are the display
// titles, invoicesListKeys the JSON wire keys each column reads.
var (
	invoicesListHeaders = []string{"id", "customer_id", "number", "amount", "status", "issued_on", "due_on", "paid_on"}
	invoicesListKeys    = []string{"id", "customerId", "number", "amount", "status", "issuedOn", "dueOn", "paidOn"}
)

func runInvoicesList(args []string) int {
	return runListVerb("invoices list", "/invoices", invoicesListFilters, invoicesListHeaders, invoicesListKeys, args)
}

func runInvoicesGet(args []string) int {
	return runGetVerb("invoices get", "/invoices", args)
}

// invoicesMutationFields is the field-flag table behind `invoices create/update/patch`:
// one entry per writable field, each bound to the JSON wire key it sets.
var invoicesMutationFields = []mutationField{
	{flag: "customer-id", wire: "customerId", kind: fieldString, usage: "customer_id (relation)"},
	{flag: "number", wire: "number", kind: fieldString, usage: "number (string)"},
	{flag: "amount", wire: "amount", kind: fieldString, usage: "amount (decimal)"},
	{flag: "status", wire: "status", kind: fieldString, usage: "status (enum) [draft|open|paid|past_due|void]"},
	{flag: "issued-on", wire: "issuedOn", kind: fieldString, usage: "issued_on (date)"},
	{flag: "due-on", wire: "dueOn", kind: fieldString, usage: "due_on (date)"},
	{flag: "paid-on", wire: "paidOn", kind: fieldString, usage: "paid_on (date)"},
}

func runInvoicesCreate(args []string) int {
	return runCreateVerb("invoices create", "/invoices", invoicesMutationFields, args)
}

func runInvoicesUpdate(args []string) int {
	return runUpdateVerb("invoices update", "/invoices", invoicesMutationFields, args)
}

func runInvoicesPatch(args []string) int {
	return runPatchVerb("invoices patch", "/invoices", invoicesMutationFields, args)
}

func runInvoicesDelete(args []string) int {
	return runDeleteVerb("invoices delete", "/invoices", args)
}

func runInvoicesBatchCreate(args []string) int {
	return runBatchJSONVerb("invoices batch-create", "/invoices", http.MethodPost, args)
}

func runInvoicesBatchUpdate(args []string) int {
	return runBatchJSONVerb("invoices batch-update", "/invoices", http.MethodPatch, args)
}

func runInvoicesBatchDelete(args []string) int {
	return runBatchDeleteVerb("invoices batch-delete", "/invoices", args)
}

func runInvoicesWatch(args []string) int {
	return runWatchVerb("invoices watch", (*client.Client).WatchInvoices, args)
}

func runInvoicesMarkPaid(args []string) int {
	return runTransitionVerb("invoices mark_paid", "/invoices", "mark_paid", args)
}

func runInvoicesVoid(args []string) int {
	return runTransitionVerb("invoices void", "/invoices", "void", args)
}
