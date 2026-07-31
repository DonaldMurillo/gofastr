// Package email sends transactional email over SMTP and renders it from
// templates. It is a plain library: construct a Sender (SMTPSender for
// production, LogSender for development) and call Send from a hook, a
// queue worker, or a handler. Templates render the subject and text body
// with text/template and the HTML body with html/template, and load from
// a directory or an fs.FS.
//
// See the reference page: framework/docs/content/email.md.
package email
