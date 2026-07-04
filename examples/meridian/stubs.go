package main

// seedEntity is one entity's ordered seed rows.
type seedEntity struct {
	Entity string
	Rows   []map[string]any
}

// seedData returns the initial seed data in blueprint-declared
// order (so entities that reference others are inserted after them).
func seedData() []seedEntity {
	return []seedEntity{
		{Entity: "plans", Rows: []map[string]any{
			{"active": true, "interval": "month", "name": "Starter", "price": "29", "slug": "starter"},
			{"active": true, "interval": "month", "name": "Pro", "price": "99", "slug": "pro"},
			{"active": true, "interval": "month", "name": "Scale", "price": "299", "slug": "scale"},
		}},
		{Entity: "customers", Rows: []map[string]any{
			{"company": "Acme Inc", "email": "ada@acme.io", "mrr": "99", "name": "Ada Lovelace", "status": "active"},
			{"company": "Globex", "email": "grace@globex.com", "mrr": "299", "name": "Grace Hopper", "status": "active"},
			{"company": "Initech", "email": "alan@initech.io", "mrr": "29", "name": "Alan Turing", "status": "past_due"},
			{"company": "Umbrella", "email": "kj@umbrella.co", "mrr": "99", "name": "Katherine Johnson", "status": "active"},
			{"company": "Hooli", "email": "linus@hooli.com", "mrr": "0", "name": "Linus Torvalds", "status": "canceled"},
			{"company": "Apollo", "email": "mh@apollo.io", "mrr": "299", "name": "Margaret Hamilton", "status": "active"},
			{"company": "Bell Labs", "email": "dmr@belllabs.com", "mrr": "0", "name": "Dennis Ritchie", "status": "trialing"},
			{"company": "MIT", "email": "liskov@mit.edu", "mrr": "99", "name": "Barbara Liskov", "status": "active"},
			{"company": "W3C", "email": "tbl@w3.org", "mrr": "29", "name": "Tim Berners-Lee", "status": "past_due"},
			{"company": "Stanford", "email": "knuth@stanford.edu", "mrr": "299", "name": "Donald Knuth", "status": "active"},
		}},
		{Entity: "subscriptions", Rows: []map[string]any{
			{"customer_id": "@customers.email=ada@acme.io", "mrr": "99", "plan_id": "@plans.slug=pro", "renews_on": "2026-07-04", "started_on": "2025-09-04", "status": "active"},
			{"customer_id": "@customers.email=grace@globex.com", "mrr": "299", "plan_id": "@plans.slug=scale", "renews_on": "2026-06-12", "started_on": "2025-06-12", "status": "active"},
			{"customer_id": "@customers.email=alan@initech.io", "mrr": "29", "plan_id": "@plans.slug=starter", "renews_on": "2026-05-20", "started_on": "2025-11-20", "status": "past_due"},
			{"customer_id": "@customers.email=kj@umbrella.co", "mrr": "99", "plan_id": "@plans.slug=pro", "renews_on": "2026-07-01", "started_on": "2025-10-01", "status": "active"},
			{"customer_id": "@customers.email=linus@hooli.com", "mrr": "0", "plan_id": "@plans.slug=scale", "renews_on": "2025-12-15", "started_on": "2024-12-15", "status": "canceled"},
			{"customer_id": "@customers.email=mh@apollo.io", "mrr": "299", "plan_id": "@plans.slug=scale", "renews_on": "2026-06-22", "started_on": "2025-03-22", "status": "active"},
			{"customer_id": "@customers.email=dmr@belllabs.com", "mrr": "0", "plan_id": "@plans.slug=pro", "renews_on": "2026-06-15", "started_on": "2026-06-01", "status": "trialing"},
			{"customer_id": "@customers.email=liskov@mit.edu", "mrr": "99", "plan_id": "@plans.slug=pro", "renews_on": "2026-07-09", "started_on": "2025-08-09", "status": "active"},
			{"customer_id": "@customers.email=tbl@w3.org", "mrr": "29", "plan_id": "@plans.slug=starter", "renews_on": "2026-06-02", "started_on": "2025-12-02", "status": "past_due"},
			{"customer_id": "@customers.email=knuth@stanford.edu", "mrr": "299", "plan_id": "@plans.slug=scale", "renews_on": "2026-06-18", "started_on": "2025-05-18", "status": "active"},
		}},
		{Entity: "invoices", Rows: []map[string]any{
			{"amount": "99", "customer_id": "@customers.email=ada@acme.io", "due_on": "2026-05-18", "issued_on": "2026-05-04", "number": "INV-1001", "paid_on": "2026-05-06", "status": "paid"},
			{"amount": "299", "customer_id": "@customers.email=grace@globex.com", "due_on": "2026-05-26", "issued_on": "2026-05-12", "number": "INV-1002", "paid_on": "2026-05-13", "status": "paid"},
			{"amount": "29", "customer_id": "@customers.email=alan@initech.io", "due_on": "2026-05-04", "issued_on": "2026-04-20", "number": "INV-1003", "status": "past_due"},
			{"amount": "99", "customer_id": "@customers.email=kj@umbrella.co", "due_on": "2026-05-15", "issued_on": "2026-05-01", "number": "INV-1004", "paid_on": "2026-05-02", "status": "paid"},
			{"amount": "299", "customer_id": "@customers.email=mh@apollo.io", "due_on": "2026-06-15", "issued_on": "2026-06-01", "number": "INV-1005", "status": "open"},
			{"amount": "99", "customer_id": "@customers.email=liskov@mit.edu", "due_on": "2026-05-23", "issued_on": "2026-05-09", "number": "INV-1006", "paid_on": "2026-05-11", "status": "paid"},
			{"amount": "29", "customer_id": "@customers.email=tbl@w3.org", "due_on": "2026-04-16", "issued_on": "2026-04-02", "number": "INV-1007", "status": "past_due"},
			{"amount": "299", "customer_id": "@customers.email=knuth@stanford.edu", "due_on": "2026-06-01", "issued_on": "2026-05-18", "number": "INV-1008", "paid_on": "2026-05-19", "status": "paid"},
			{"amount": "299", "customer_id": "@customers.email=grace@globex.com", "due_on": "2026-06-26", "issued_on": "2026-06-12", "number": "INV-1009", "status": "open"},
			{"amount": "99", "customer_id": "@customers.email=ada@acme.io", "due_on": "2026-06-18", "issued_on": "2026-06-04", "number": "INV-1010", "status": "open"},
			{"amount": "299", "customer_id": "@customers.email=knuth@stanford.edu", "due_on": "2026-05-02", "issued_on": "2026-04-18", "number": "INV-1011", "paid_on": "2026-04-20", "status": "paid"},
			{"amount": "99", "customer_id": "@customers.email=kj@umbrella.co", "due_on": "2026-04-29", "issued_on": "2026-04-15", "number": "INV-1012", "status": "past_due"},
		}},
	}
}
