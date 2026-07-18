# Security policy

GoFastr's pitch is secure-by-default. That only means something if reporting
a hole is easy and the track record is public. Here is both.

## Reporting a vulnerability

**Please do not open a public issue for a security bug.**

Report it privately via GitHub's private vulnerability reporting:
[Security → Report a vulnerability](https://github.com/DonaldMurillo/gofastr/security/advisories/new)
on `DonaldMurillo/gofastr`.

If the report form is unavailable for any reason, open a regular issue that
says only "security report — need a private channel" with **no details**, and
a private channel will be arranged.

## What to expect

This project has a single maintainer. There is no security team and no SLA.
Honest expectations:

- Reports are acknowledged on a best-effort basis, usually within days.
- Confirmed vulnerabilities in supported code are prioritized over feature work.
- You'll be credited in the fix/advisory unless you ask otherwise.

## Supported versions

GoFastr is pre-1.0. Only the **latest minor release** (currently `0.32.x`)
receives security fixes. Older `0.x` lines are not patched — upgrade to the
latest release to stay supported.

## Audit trail

The codebase has been through repeated adversarial audit campaigns (100+
verified findings, each of which survived a refute pass — all fixed and
pinned by `*_security_test.go` tests throughout the tree). The full
finding-by-finding ledger lives in git history (`SECURITY_FINDINGS.md`,
removed 2026-07-15 after every row was re-verified fixed). Release gates
include `make security-full` (gofmt, vet, secret scan, race tests,
`govulncheck`, module verification).
