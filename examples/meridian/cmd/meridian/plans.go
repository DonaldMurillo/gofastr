package main

import (
	"net/http"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func plansCommands() []command {
	return []command{
		{name: "plans", summary: "manage plans (run for subcommands)", run: func(args []string) int { return groupUsage("plans", args) }},
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

// Verb wrappers: each binds this entity's command names and pre-escaped
// base path "/plans" to the shared verb bodies in verbs.go.

// plansListFilters is the filter-flag table behind `plans list`: one entry
// per flag, in help order, each bound to the query param it sets.
var plansListFilters = []filterFlag{
	{flag: "name", param: "name", help: "filter: name equals (comma list = IN)"},
	{flag: "name-like", param: "name_like", help: "filter: name contains"},
	{flag: "slug", param: "slug", help: "filter: slug equals (comma list = IN)"},
	{flag: "slug-like", param: "slug_like", help: "filter: slug contains"},
	{flag: "price", param: "price", help: "filter: price equals (comma list = IN)"},
	{flag: "price-gt", param: "price_gt", help: "filter: price gt"},
	{flag: "price-gte", param: "price_gte", help: "filter: price gte"},
	{flag: "price-lt", param: "price_lt", help: "filter: price lt"},
	{flag: "price-lte", param: "price_lte", help: "filter: price lte"},
	{flag: "interval", param: "interval", help: "filter: interval equals (comma list = IN) [month|year]"},
	{flag: "active", param: "active", help: "filter: active equals (comma list = IN)"},
}

// Table columns for `plans list -o table`: plansListHeaders are the display
// titles, plansListKeys the JSON wire keys each column reads.
var (
	plansListHeaders = []string{"id", "name", "slug", "price", "interval", "active"}
	plansListKeys    = []string{"id", "name", "slug", "price", "interval", "active"}
)

func runPlansList(args []string) int {
	return runListVerb("plans list", "/plans", plansListFilters, plansListHeaders, plansListKeys, args)
}

func runPlansGet(args []string) int {
	return runGetVerb("plans get", "/plans", args)
}

// plansMutationFields is the field-flag table behind `plans create/update/patch`:
// one entry per writable field, each bound to the JSON wire key it sets.
var plansMutationFields = []mutationField{
	{flag: "name", wire: "name", kind: fieldString, usage: "name (string)"},
	{flag: "slug", wire: "slug", kind: fieldString, usage: "slug (string)"},
	{flag: "price", wire: "price", kind: fieldString, usage: "price (decimal)"},
	{flag: "interval", wire: "interval", kind: fieldString, usage: "interval (enum) [month|year]"},
	{flag: "active", wire: "active", kind: fieldBool, usage: "active (bool)"},
}

func runPlansCreate(args []string) int {
	return runCreateVerb("plans create", "/plans", plansMutationFields, args)
}

func runPlansUpdate(args []string) int {
	return runUpdateVerb("plans update", "/plans", plansMutationFields, args)
}

func runPlansPatch(args []string) int {
	return runPatchVerb("plans patch", "/plans", plansMutationFields, args)
}

func runPlansDelete(args []string) int {
	return runDeleteVerb("plans delete", "/plans", args)
}

func runPlansBatchCreate(args []string) int {
	return runBatchJSONVerb("plans batch-create", "/plans", http.MethodPost, args)
}

func runPlansBatchUpdate(args []string) int {
	return runBatchJSONVerb("plans batch-update", "/plans", http.MethodPatch, args)
}

func runPlansBatchDelete(args []string) int {
	return runBatchDeleteVerb("plans batch-delete", "/plans", args)
}

func runPlansWatch(args []string) int {
	return runWatchVerb("plans watch", (*client.Client).WatchPlans, args)
}
