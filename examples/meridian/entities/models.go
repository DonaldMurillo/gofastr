package entities

type Customers struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Company string `json:"company,omitempty"`
	Status  string `json:"status,omitempty"`
	Mrr     string `json:"mrr,omitempty"`
}

type Invoices struct {
	ID         string     `json:"id"`
	CustomerId string     `json:"customerId,omitempty"`
	Number     string     `json:"number,omitempty"`
	Amount     string     `json:"amount,omitempty"`
	Status     string     `json:"status,omitempty"`
	IssuedOn   string     `json:"issuedOn,omitempty"`
	DueOn      string     `json:"dueOn,omitempty"`
	PaidOn     string     `json:"paidOn,omitempty"`
	Customer   *Customers `json:"customer,omitempty"`
}

type Payments struct {
	ID         string     `json:"id"`
	InvoiceId  string     `json:"invoiceId,omitempty"`
	CustomerId string     `json:"customerId,omitempty"`
	Amount     string     `json:"amount,omitempty"`
	Method     string     `json:"method,omitempty"`
	Status     string     `json:"status,omitempty"`
	Invoice    *Invoices  `json:"invoice,omitempty"`
	Customer   *Customers `json:"customer,omitempty"`
}

type Plans struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Price    string `json:"price,omitempty"`
	Interval string `json:"interval,omitempty"`
	Active   bool   `json:"active,omitempty"`
}

type Subscriptions struct {
	ID         string     `json:"id"`
	CustomerId string     `json:"customerId,omitempty"`
	PlanId     string     `json:"planId,omitempty"`
	Status     string     `json:"status,omitempty"`
	Mrr        string     `json:"mrr,omitempty"`
	StartedOn  string     `json:"startedOn,omitempty"`
	RenewsOn   string     `json:"renewsOn,omitempty"`
	Customer   *Customers `json:"customer,omitempty"`
	Plan       *Plans     `json:"plan,omitempty"`
}
