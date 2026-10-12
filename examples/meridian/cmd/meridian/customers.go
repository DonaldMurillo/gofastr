package main

import (
	"net/http"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func customersCommands() []command {
	return []command{
		{name: "customers", summary: "manage customers (run for subcommands)", run: func(args []string) int { return groupUsage("customers", args) }},
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

// Verb wrappers: each binds this entity's command names and pre-escaped
// base path "/customers" to the shared verb bodies in verbs.go.

// customersListFilters is the filter-flag table behind `customers list`: one entry
// per flag, in help order, each bound to the query param it sets.
var customersListFilters = []filterFlag{
	{flag: "q", param: "q", help: "free-text search over the declared search fields"},
	{flag: "name", param: "name", help: "filter: name equals (comma list = IN)"},
	{flag: "name-gte", param: "name_gte", help: "filter: name greater than or equal"},
	{flag: "name-lte", param: "name_lte", help: "filter: name less than or equal"},
	{flag: "name-gt", param: "name_gt", help: "filter: name greater than"},
	{flag: "name-lt", param: "name_lt", help: "filter: name less than"},
	{flag: "name-like", param: "name_like", help: "filter: name contains"},
	{flag: "name-ne", param: "name_ne", help: "filter: name not equal"},
	{flag: "email", param: "email", help: "filter: email equals (comma list = IN)"},
	{flag: "email-gte", param: "email_gte", help: "filter: email greater than or equal"},
	{flag: "email-lte", param: "email_lte", help: "filter: email less than or equal"},
	{flag: "email-gt", param: "email_gt", help: "filter: email greater than"},
	{flag: "email-lt", param: "email_lt", help: "filter: email less than"},
	{flag: "email-like", param: "email_like", help: "filter: email contains"},
	{flag: "email-ne", param: "email_ne", help: "filter: email not equal"},
	{flag: "company", param: "company", help: "filter: company equals (comma list = IN)"},
	{flag: "company-gte", param: "company_gte", help: "filter: company greater than or equal"},
	{flag: "company-lte", param: "company_lte", help: "filter: company less than or equal"},
	{flag: "company-gt", param: "company_gt", help: "filter: company greater than"},
	{flag: "company-lt", param: "company_lt", help: "filter: company less than"},
	{flag: "company-like", param: "company_like", help: "filter: company contains"},
	{flag: "company-ne", param: "company_ne", help: "filter: company not equal"},
	{flag: "status", param: "status", help: "filter: status equals (comma list = IN) [trialing|active|past_due|canceled]"},
	{flag: "status-gte", param: "status_gte", help: "filter: status greater than or equal"},
	{flag: "status-lte", param: "status_lte", help: "filter: status less than or equal"},
	{flag: "status-gt", param: "status_gt", help: "filter: status greater than"},
	{flag: "status-lt", param: "status_lt", help: "filter: status less than"},
	{flag: "status-like", param: "status_like", help: "filter: status contains"},
	{flag: "status-ne", param: "status_ne", help: "filter: status not equal"},
	{flag: "mrr", param: "mrr", help: "filter: mrr equals (comma list = IN)"},
	{flag: "mrr-gte", param: "mrr_gte", help: "filter: mrr greater than or equal"},
	{flag: "mrr-lte", param: "mrr_lte", help: "filter: mrr less than or equal"},
	{flag: "mrr-gt", param: "mrr_gt", help: "filter: mrr greater than"},
	{flag: "mrr-lt", param: "mrr_lt", help: "filter: mrr less than"},
	{flag: "mrr-ne", param: "mrr_ne", help: "filter: mrr not equal"},
}

// Table columns for `customers list -o table`: customersListHeaders are the display
// titles, customersListKeys the JSON wire keys each column reads.
var (
	customersListHeaders = []string{"id", "name", "email", "company", "status", "mrr"}
	customersListKeys    = []string{"id", "name", "email", "company", "status", "mrr"}
)

func runCustomersList(args []string) int {
	return runListVerb("customers list", "/customers", customersListFilters, customersListHeaders, customersListKeys, args)
}

func runCustomersGet(args []string) int {
	return runGetVerb("customers get", "/customers", args)
}

// customersMutationFields is the field-flag table behind `customers create/update/patch`:
// one entry per writable field, each bound to the JSON wire key it sets.
var customersMutationFields = []mutationField{
	{flag: "name", wire: "name", kind: fieldString, usage: "name (string)"},
	{flag: "email", wire: "email", kind: fieldString, usage: "email (string)"},
	{flag: "company", wire: "company", kind: fieldString, usage: "company (string)"},
	{flag: "status", wire: "status", kind: fieldString, usage: "status (enum) [trialing|active|past_due|canceled]"},
	{flag: "mrr", wire: "mrr", kind: fieldString, usage: "mrr (decimal)"},
}

func runCustomersCreate(args []string) int {
	return runCreateVerb("customers create", "/customers", customersMutationFields, args)
}

func runCustomersUpdate(args []string) int {
	return runUpdateVerb("customers update", "/customers", customersMutationFields, args)
}

func runCustomersPatch(args []string) int {
	return runPatchVerb("customers patch", "/customers", customersMutationFields, args)
}

func runCustomersDelete(args []string) int {
	return runDeleteVerb("customers delete", "/customers", args)
}

func runCustomersBatchCreate(args []string) int {
	return runBatchJSONVerb("customers batch-create", "/customers", http.MethodPost, args)
}

func runCustomersBatchUpdate(args []string) int {
	return runBatchJSONVerb("customers batch-update", "/customers", http.MethodPatch, args)
}

func runCustomersBatchDelete(args []string) int {
	return runBatchDeleteVerb("customers batch-delete", "/customers", args)
}

func runCustomersWatch(args []string) int {
	return runWatchVerb("customers watch", (*client.Client).WatchCustomers, args)
}
