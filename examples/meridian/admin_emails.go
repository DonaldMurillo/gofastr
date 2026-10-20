package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/admin"
	"github.com/DonaldMurillo/gofastr/battery/email"
)

// Meridian's mails, as the admin's Emails page previews them: the
// welcome a new account gets and the invoice mail the billing queue
// sends. Each renders from a fixed sample, so the page never reads a
// real customer.

var welcomeMail = email.Template{
	Subject:  "Welcome to Meridian, {{.name}}",
	TextBody: "Hi {{.name}},\n\nYour workspace {{.workspace}} is ready. Sign in at {{.url}} to add your first customer.\n\nThe Meridian team",
	HTMLBody: `<h1>Welcome, {{.name}}</h1>
<p>Your workspace <strong>{{.workspace}}</strong> is ready.</p>
<p><a href="{{.url}}">Sign in</a> to add your first customer.</p>
<p>The Meridian team</p>`,
}

var invoiceMail = email.Template{
	Subject:  "Invoice {{.number}} from Meridian",
	TextBody: "Invoice {{.number}} for {{.amount}} is due on {{.due}}.\n\nView it at {{.url}}.",
	HTMLBody: `<h1>Invoice {{.number}}</h1>
<table>
<tr><th>Amount</th><td>{{.amount}}</td></tr>
<tr><th>Due</th><td>{{.due}}</td></tr>
</table>
<p><a href="{{.url}}">View the invoice</a></p>`,
}

// adminEmails are the previews the admin's Emails page draws.
func adminEmails() []admin.EmailPreview {
	return []admin.EmailPreview{
		{Name: "welcome", Render: func(context.Context) (email.Email, error) {
			return email.Execute(welcomeMail, map[string]any{
				"name": "Ada", "workspace": "Acme Corp", "url": "https://meridian.example/login",
			})
		}},
		{Name: "invoice", Render: func(context.Context) (email.Email, error) {
			return email.Execute(invoiceMail, map[string]any{
				"number": "INV-1042", "amount": "$1,240.00", "due": "1 Nov 2026", "url": "https://meridian.example/invoices/1042",
			})
		}},
	}
}
