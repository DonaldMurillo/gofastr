package blueprint

// BlueprintSeedEntity is one entity's ordered seed rows.
type BlueprintSeedEntity struct {
	Entity string
	Rows   []map[string]any
}

// BlueprintSeedData returns the initial seed data in blueprint-declared
// order (so entities that reference others are inserted after them).
func BlueprintSeedData() []BlueprintSeedEntity {
	return []BlueprintSeedEntity{
		{Entity: "plans", Rows: []map[string]any{
			{"name": "Starter", "price": "29", "interval": "month", "active": true, "slug": "starter"},
			{"slug": "pro", "price": "99", "name": "Pro", "interval": "month", "active": true},
			{"price": "299", "name": "Scale", "interval": "month", "active": true, "slug": "scale"},
		}},
		{Entity: "customers", Rows: []map[string]any{
			{"name": "Ada Lovelace", "status": "active", "mrr": "99", "email": "ada@acme.io", "company": "Acme Inc"},
			{"email": "grace@globex.com", "company": "Globex", "status": "active", "name": "Grace Hopper", "mrr": "299"},
			{"company": "Initech", "name": "Alan Turing", "status": "past_due", "mrr": "29", "email": "alan@initech.io"},
			{"mrr": "99", "name": "Katherine Johnson", "email": "kj@umbrella.co", "company": "Umbrella", "status": "active"},
			{"company": "Hooli", "status": "canceled", "mrr": "0", "name": "Linus Torvalds", "email": "linus@hooli.com"},
			{"company": "Apollo", "status": "active", "mrr": "299", "name": "Margaret Hamilton", "email": "mh@apollo.io"},
			{"name": "Dennis Ritchie", "email": "dmr@belllabs.com", "company": "Bell Labs", "status": "trialing", "mrr": "0"},
			{"name": "Barbara Liskov", "email": "liskov@mit.edu", "company": "MIT", "status": "active", "mrr": "99"},
			{"name": "Tim Berners-Lee", "email": "tbl@w3.org", "company": "W3C", "status": "past_due", "mrr": "29"},
			{"name": "Donald Knuth", "email": "knuth@stanford.edu", "company": "Stanford", "status": "active", "mrr": "299"},
		}},
		{Entity: "subscriptions", Rows: []map[string]any{
			{"mrr": "99", "started_on": "2025-09-04", "renews_on": "2026-07-04", "plan_id": "@plans.slug=pro", "customer_id": "@customers.email=ada@acme.io", "status": "active"},
			{"renews_on": "2026-06-12", "plan_id": "@plans.slug=scale", "customer_id": "@customers.email=grace@globex.com", "status": "active", "mrr": "299", "started_on": "2025-06-12"},
			{"status": "past_due", "mrr": "29", "started_on": "2025-11-20", "renews_on": "2026-05-20", "customer_id": "@customers.email=alan@initech.io", "plan_id": "@plans.slug=starter"},
			{"customer_id": "@customers.email=kj@umbrella.co", "renews_on": "2026-07-01", "plan_id": "@plans.slug=pro", "status": "active", "mrr": "99", "started_on": "2025-10-01"},
			{"customer_id": "@customers.email=linus@hooli.com", "mrr": "0", "started_on": "2024-12-15", "renews_on": "2025-12-15", "plan_id": "@plans.slug=scale", "status": "canceled"},
			{"renews_on": "2026-06-22", "customer_id": "@customers.email=mh@apollo.io", "plan_id": "@plans.slug=scale", "status": "active", "mrr": "299", "started_on": "2025-03-22"},
			{"plan_id": "@plans.slug=pro", "status": "trialing", "mrr": "0", "started_on": "2026-06-01", "customer_id": "@customers.email=dmr@belllabs.com", "renews_on": "2026-06-15"},
			{"plan_id": "@plans.slug=pro", "status": "active", "mrr": "99", "started_on": "2025-08-09", "renews_on": "2026-07-09", "customer_id": "@customers.email=liskov@mit.edu"},
			{"plan_id": "@plans.slug=starter", "status": "past_due", "mrr": "29", "started_on": "2025-12-02", "renews_on": "2026-06-02", "customer_id": "@customers.email=tbl@w3.org"},
			{"customer_id": "@customers.email=knuth@stanford.edu", "started_on": "2025-05-18", "renews_on": "2026-06-18", "plan_id": "@plans.slug=scale", "status": "active", "mrr": "299"},
		}},
		{Entity: "invoices", Rows: []map[string]any{
			{"issued_on": "2026-05-04", "due_on": "2026-05-18", "customer_id": "@customers.email=ada@acme.io", "paid_on": "2026-05-06", "number": "INV-1001", "amount": "99", "status": "paid"},
			{"issued_on": "2026-05-12", "due_on": "2026-05-26", "paid_on": "2026-05-13", "customer_id": "@customers.email=grace@globex.com", "number": "INV-1002", "amount": "299", "status": "paid"},
			{"status": "past_due", "customer_id": "@customers.email=alan@initech.io", "issued_on": "2026-04-20", "due_on": "2026-05-04", "number": "INV-1003", "amount": "29"},
			{"issued_on": "2026-05-01", "due_on": "2026-05-15", "paid_on": "2026-05-02", "number": "INV-1004", "amount": "99", "status": "paid", "customer_id": "@customers.email=kj@umbrella.co"},
			{"number": "INV-1005", "amount": "299", "status": "open", "issued_on": "2026-06-01", "customer_id": "@customers.email=mh@apollo.io", "due_on": "2026-06-15"},
			{"status": "paid", "issued_on": "2026-05-09", "due_on": "2026-05-23", "paid_on": "2026-05-11", "customer_id": "@customers.email=liskov@mit.edu", "number": "INV-1006", "amount": "99"},
			{"due_on": "2026-04-16", "number": "INV-1007", "customer_id": "@customers.email=tbl@w3.org", "amount": "29", "status": "past_due", "issued_on": "2026-04-02"},
			{"customer_id": "@customers.email=knuth@stanford.edu", "number": "INV-1008", "amount": "299", "status": "paid", "issued_on": "2026-05-18", "due_on": "2026-06-01", "paid_on": "2026-05-19"},
			{"status": "open", "customer_id": "@customers.email=grace@globex.com", "issued_on": "2026-06-12", "due_on": "2026-06-26", "number": "INV-1009", "amount": "299"},
			{"number": "INV-1010", "amount": "99", "status": "open", "issued_on": "2026-06-04", "due_on": "2026-06-18", "customer_id": "@customers.email=ada@acme.io"},
			{"due_on": "2026-05-02", "paid_on": "2026-04-20", "number": "INV-1011", "customer_id": "@customers.email=knuth@stanford.edu", "amount": "299", "status": "paid", "issued_on": "2026-04-18"},
			{"customer_id": "@customers.email=kj@umbrella.co", "issued_on": "2026-04-15", "due_on": "2026-04-29", "number": "INV-1012", "amount": "99", "status": "past_due"},
		}},
	}
}
