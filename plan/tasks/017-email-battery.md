# 017 — Email Battery

**Phase:** 2 (Batteries) | **Depends on:** 004

## Goal
Pluggable email. Interface in core, SMTP implementation in battery.

## Deliverables
- [ ] `Email` interface: `Send(ctx, to, subject, htmlBody, textBody string, opts ...EmailOption) error`
- [ ] `SMTPSender` implementation using `net/smtp`
- [ ] Email template system: register HTML + text templates, render with data
- [ ] `SendTemplate(ctx, to, templateName, data)` — render + send in one call
- [ ] Email queue: optional async send via Queue battery
- [ ] Dev mode: log emails instead of sending (inspect in console)
- [ ] Rate limiting per recipient (configurable)

## Acceptance Criteria
- SMTP implementation sends real email (test with MailHog/mailpit)
- Templates render HTML + text correctly
- Dev mode logs email without sending
- Rate limit prevents burst sends
