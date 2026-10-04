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
	{flag: "name", param: "name", help: "filter: name equals (comma list = IN)"},
	{flag: "name-like", param: "name_like", help: "filter: name contains"},
	{flag: "email", param: "email", help: "filter: email equals (comma list = IN)"},
	{flag: "email-like", param: "email_like", help: "filter: email contains"},
	{flag: "company", param: "company", help: "filter: company equals (comma list = IN)"},
	{flag: "company-like", param: "company_like", help: "filter: company contains"},
	{flag: "status", param: "status", help: "filter: status equals (comma list = IN) [trialing|active|past_due|canceled]"},
	{flag: "mrr", param: "mrr", help: "filter: mrr equals (comma list = IN)"},
	{flag: "mrr-gt", param: "mrr_gt", help: "filter: mrr gt"},
	{flag: "mrr-gte", param: "mrr_gte", help: "filter: mrr gte"},
	{flag: "mrr-lt", param: "mrr_lt", help: "filter: mrr lt"},
	{flag: "mrr-lte", param: "mrr_lte", help: "filter: mrr lte"},
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
