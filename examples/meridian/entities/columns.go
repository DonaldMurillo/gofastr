package entities

import (
	"github.com/DonaldMurillo/gofastr/framework"
)

// ====== Plans column references ======

var (
	PlansID       = framework.NewUUIDColumn("id")
	PlansName     = framework.NewStringColumn("name")
	PlansSlug     = framework.NewStringColumn("slug")
	PlansPrice    = framework.NewFloatColumn("price")
	PlansInterval = framework.NewStringColumn("interval")
	PlansActive   = framework.NewBoolColumn("active")
)

// ====== Customers column references ======

var (
	CustomersID      = framework.NewUUIDColumn("id")
	CustomersName    = framework.NewStringColumn("name")
	CustomersEmail   = framework.NewStringColumn("email")
	CustomersCompany = framework.NewStringColumn("company")
	CustomersStatus  = framework.NewStringColumn("status")
	CustomersMrr     = framework.NewFloatColumn("mrr")
	CustomersUserId  = framework.NewStringColumn("user_id")
)

// ====== Subscriptions column references ======

var (
	SubscriptionsID         = framework.NewUUIDColumn("id")
	SubscriptionsCustomerId = framework.NewUUIDColumn("customer_id")
	SubscriptionsPlanId     = framework.NewUUIDColumn("plan_id")
	SubscriptionsStatus     = framework.NewStringColumn("status")
	SubscriptionsMrr        = framework.NewFloatColumn("mrr")
	SubscriptionsStartedOn  = framework.NewTimestampColumn("started_on")
	SubscriptionsRenewsOn   = framework.NewTimestampColumn("renews_on")
	SubscriptionsUserId     = framework.NewStringColumn("user_id")
)

// Subscriptions include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	SubscriptionsInclCustomer = "customer"
	SubscriptionsInclPlan     = "plan"
)

// ====== Invoices column references ======

var (
	InvoicesID         = framework.NewUUIDColumn("id")
	InvoicesCustomerId = framework.NewUUIDColumn("customer_id")
	InvoicesNumber     = framework.NewStringColumn("number")
	InvoicesAmount     = framework.NewFloatColumn("amount")
	InvoicesStatus     = framework.NewStringColumn("status")
	InvoicesIssuedOn   = framework.NewTimestampColumn("issued_on")
	InvoicesDueOn      = framework.NewTimestampColumn("due_on")
	InvoicesPaidOn     = framework.NewTimestampColumn("paid_on")
	InvoicesUserId     = framework.NewStringColumn("user_id")
)

// Invoices include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	InvoicesInclCustomer = "customer"
)

// ====== Payments column references ======

var (
	PaymentsID         = framework.NewUUIDColumn("id")
	PaymentsInvoiceId  = framework.NewUUIDColumn("invoice_id")
	PaymentsCustomerId = framework.NewUUIDColumn("customer_id")
	PaymentsAmount     = framework.NewFloatColumn("amount")
	PaymentsMethod     = framework.NewStringColumn("method")
	PaymentsStatus     = framework.NewStringColumn("status")
	PaymentsUserId     = framework.NewStringColumn("user_id")
)

// Payments include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	PaymentsInclInvoice  = "invoice"
	PaymentsInclCustomer = "customer"
)
