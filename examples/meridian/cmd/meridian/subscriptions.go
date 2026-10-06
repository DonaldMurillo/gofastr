package main

import (
	"net/http"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

func subscriptionsCommands() []command {
	return []command{
		{name: "subscriptions", summary: "manage subscriptions (run for subcommands)", run: func(args []string) int { return groupUsage("subscriptions", args) }},
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
		{name: "subscriptions activate", summary: "move status from trialing or past_due to active", run: runSubscriptionsActivate},
		{name: "subscriptions cancel", summary: "move status from trialing or active or past_due to canceled", run: runSubscriptionsCancel},
	}
}

// Verb wrappers: each binds this entity's command names and pre-escaped
// base path "/subscriptions" to the shared verb bodies in verbs.go.

// subscriptionsListFilters is the filter-flag table behind `subscriptions list`: one entry
// per flag, in help order, each bound to the query param it sets.
var subscriptionsListFilters = []filterFlag{
	{flag: "customer-id", param: "customer_id", help: "filter: customer_id equals (comma list = IN)"},
	{flag: "customer-id-gte", param: "customer_id_gte", help: "filter: customer_id greater than or equal"},
	{flag: "customer-id-lte", param: "customer_id_lte", help: "filter: customer_id less than or equal"},
	{flag: "customer-id-gt", param: "customer_id_gt", help: "filter: customer_id greater than"},
	{flag: "customer-id-lt", param: "customer_id_lt", help: "filter: customer_id less than"},
	{flag: "customer-id-ne", param: "customer_id_ne", help: "filter: customer_id not equal"},
	{flag: "plan-id", param: "plan_id", help: "filter: plan_id equals (comma list = IN)"},
	{flag: "plan-id-gte", param: "plan_id_gte", help: "filter: plan_id greater than or equal"},
	{flag: "plan-id-lte", param: "plan_id_lte", help: "filter: plan_id less than or equal"},
	{flag: "plan-id-gt", param: "plan_id_gt", help: "filter: plan_id greater than"},
	{flag: "plan-id-lt", param: "plan_id_lt", help: "filter: plan_id less than"},
	{flag: "plan-id-ne", param: "plan_id_ne", help: "filter: plan_id not equal"},
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
	{flag: "started-on", param: "started_on", help: "filter: started_on equals (comma list = IN)"},
	{flag: "started-on-gte", param: "started_on_gte", help: "filter: started_on greater than or equal"},
	{flag: "started-on-lte", param: "started_on_lte", help: "filter: started_on less than or equal"},
	{flag: "started-on-gt", param: "started_on_gt", help: "filter: started_on greater than"},
	{flag: "started-on-lt", param: "started_on_lt", help: "filter: started_on less than"},
	{flag: "started-on-ne", param: "started_on_ne", help: "filter: started_on not equal"},
	{flag: "renews-on", param: "renews_on", help: "filter: renews_on equals (comma list = IN)"},
	{flag: "renews-on-gte", param: "renews_on_gte", help: "filter: renews_on greater than or equal"},
	{flag: "renews-on-lte", param: "renews_on_lte", help: "filter: renews_on less than or equal"},
	{flag: "renews-on-gt", param: "renews_on_gt", help: "filter: renews_on greater than"},
	{flag: "renews-on-lt", param: "renews_on_lt", help: "filter: renews_on less than"},
	{flag: "renews-on-ne", param: "renews_on_ne", help: "filter: renews_on not equal"},
}

// Table columns for `subscriptions list -o table`: subscriptionsListHeaders are the display
// titles, subscriptionsListKeys the JSON wire keys each column reads.
var (
	subscriptionsListHeaders = []string{"id", "customer_id", "plan_id", "status", "mrr", "started_on", "renews_on"}
	subscriptionsListKeys    = []string{"id", "customerId", "planId", "status", "mrr", "startedOn", "renewsOn"}
)

func runSubscriptionsList(args []string) int {
	return runListVerb("subscriptions list", "/subscriptions", subscriptionsListFilters, subscriptionsListHeaders, subscriptionsListKeys, args)
}

func runSubscriptionsGet(args []string) int {
	return runGetVerb("subscriptions get", "/subscriptions", args)
}

// subscriptionsMutationFields is the field-flag table behind `subscriptions create/update/patch`:
// one entry per writable field, each bound to the JSON wire key it sets.
var subscriptionsMutationFields = []mutationField{
	{flag: "customer-id", wire: "customerId", kind: fieldString, usage: "customer_id (relation)"},
	{flag: "plan-id", wire: "planId", kind: fieldString, usage: "plan_id (relation)"},
	{flag: "status", wire: "status", kind: fieldString, usage: "status (enum) [trialing|active|past_due|canceled]"},
	{flag: "mrr", wire: "mrr", kind: fieldString, usage: "mrr (decimal)"},
	{flag: "started-on", wire: "startedOn", kind: fieldString, usage: "started_on (date)"},
	{flag: "renews-on", wire: "renewsOn", kind: fieldString, usage: "renews_on (date)"},
}

func runSubscriptionsCreate(args []string) int {
	return runCreateVerb("subscriptions create", "/subscriptions", subscriptionsMutationFields, args)
}

func runSubscriptionsUpdate(args []string) int {
	return runUpdateVerb("subscriptions update", "/subscriptions", subscriptionsMutationFields, args)
}

func runSubscriptionsPatch(args []string) int {
	return runPatchVerb("subscriptions patch", "/subscriptions", subscriptionsMutationFields, args)
}

func runSubscriptionsDelete(args []string) int {
	return runDeleteVerb("subscriptions delete", "/subscriptions", args)
}

func runSubscriptionsBatchCreate(args []string) int {
	return runBatchJSONVerb("subscriptions batch-create", "/subscriptions", http.MethodPost, args)
}

func runSubscriptionsBatchUpdate(args []string) int {
	return runBatchJSONVerb("subscriptions batch-update", "/subscriptions", http.MethodPatch, args)
}

func runSubscriptionsBatchDelete(args []string) int {
	return runBatchDeleteVerb("subscriptions batch-delete", "/subscriptions", args)
}

func runSubscriptionsWatch(args []string) int {
	return runWatchVerb("subscriptions watch", (*client.Client).WatchSubscriptions, args)
}

func runSubscriptionsActivate(args []string) int {
	return runTransitionVerb("subscriptions activate", "/subscriptions", "activate", args)
}

func runSubscriptionsCancel(args []string) int {
	return runTransitionVerb("subscriptions cancel", "/subscriptions", "cancel", args)
}
