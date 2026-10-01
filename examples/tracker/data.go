package main

// The in-memory model behind Acme Tracker: four projects, six people,
// twenty-five issues with real descriptions and per-issue activity.
// No database, no auth — every screen computes from these tables, so
// counts, charts, and feeds stay honest (they are derived, never
// hand-written).

import (
	"sort"
	"strconv"
	"strings"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Person is one teammate. The avatar derives initials from Name.
type Person struct {
	Name string
}

// people is the whole team.
var people = []Person{
	{Name: "Mina Fischer"},
	{Name: "Otis Vance"},
	{Name: "Priya Nandi"},
	{Name: "Jonas Weber"},
	{Name: "Alba Cruz"},
	{Name: "Theo Ito"},
}

// personByName finds a teammate; unknown names render unassigned.
func personByName(name string) (Person, bool) {
	for _, p := range people {
		if p.Name == name {
			return p, true
		}
	}
	return Person{}, false
}

// Project is one tracker project.
type Project struct {
	Slug    string // URL segment
	Name    string // display name
	Key     string // issue key prefix (BIL-42)
	Health  string // "On track", "At risk", "Maintenance"
	Tone    ui.StatusVariant
	Summary string    // one line for cards and the aside
	Trend   []float64 // opened-per-week sparkline, oldest first
}

// projects in sidebar order.
var projects = []Project{
	{
		Slug: "billing", Name: "Billing", Key: "BIL",
		Health: "On track", Tone: ui.StatusSuccess,
		Summary: "Subscriptions, invoicing, and the payment service provider integrations.",
		Trend:   []float64{2, 3, 2, 4, 3, 5, 4, 3, 4, 6, 5, 4},
	},
	{
		Slug: "search", Name: "Search", Key: "SRCH",
		Health: "At risk", Tone: ui.StatusWarning,
		Summary: "Catalog search, ranking experiments, and the query pipeline rewrite.",
		Trend:   []float64{5, 6, 4, 7, 8, 6, 9, 7, 8, 10, 9, 11},
	},
	{
		Slug: "auth", Name: "Auth", Key: "AUTH",
		Health: "On track", Tone: ui.StatusSuccess,
		Summary: "Sign-in, sessions, passkeys, and the device trust rollout.",
		Trend:   []float64{1, 2, 2, 1, 3, 2, 2, 3, 2, 1, 2, 2},
	},
	{
		Slug: "legacy", Name: "Legacy", Key: "LEG",
		Health: "Maintenance", Tone: ui.StatusNeutral,
		Summary: "The pre-2023 platform. Kept alive for two enterprise accounts; no activity feed is migrated yet.",
		Trend:   []float64{3, 2, 2, 1, 1, 2, 0, 1, 1, 0, 1, 0},
	},
}

// projectBySlug resolves a URL slug.
func projectBySlug(slug string) (Project, bool) {
	for _, p := range projects {
		if p.Slug == slug {
			return p, true
		}
	}
	return Project{}, false
}

// Issue statuses and priorities.
const (
	statusOpen     = "Open"
	statusProgress = "In progress"
	statusBlocked  = "Blocked"
	statusDone     = "Done"

	prioLow    = "Low"
	prioMedium = "Medium"
	prioHigh   = "High"
	prioUrgent = "Urgent"
)

// statusVariant maps a status onto the badge system's tones.
func statusVariant(status string) ui.StatusVariant {
	switch status {
	case statusOpen:
		return ui.StatusInfo
	case statusProgress:
		return ui.StatusNeutral
	case statusBlocked:
		return ui.StatusDanger
	case statusDone:
		return ui.StatusSuccess
	}
	return ui.StatusNeutral
}

// priorityVariant maps a priority onto tag tones.
func priorityVariant(prio string) ui.StatusVariant {
	switch prio {
	case prioLow:
		return ui.StatusNeutral
	case prioMedium:
		return ui.StatusInfo
	case prioHigh:
		return ui.StatusWarning
	case prioUrgent:
		return ui.StatusDanger
	}
	return ui.StatusNeutral
}

// Comment is one remark on an issue's activity feed.
type Comment struct {
	Author string
	On     string // "18 Sep"
	Text   string
	// Malformed marks a record whose event payload cannot be parsed —
	// a data bug on that one issue (BIL-63's third event): the
	// activity loader panics on it and the panel's ErrorBoundary
	// answers, the rest of the page unaffected.
	Malformed bool
}

// Issue is one tracker issue.
type Issue struct {
	Number     int
	Title      string
	Status     string
	Priority   string
	Assignee   string
	Reporter   string
	Created    string
	Updated    string
	Paragraphs []string
	Comments   []Comment
}

// Key renders the issue's tracker key (BIL-42).
func (i Issue) Key() string { return issueKey(i.Number) }

func issueKey(n int) string {
	p := issueProject(n)
	return p.Key + "-" + strconv.Itoa(n)
}

// issueProject finds the project owning issue number n.
func issueProject(n int) Project {
	for _, p := range projects {
		for _, iss := range issuesBySlug[p.Slug] {
			if iss.Number == n {
				return p
			}
		}
	}
	return Project{}
}

// issuesBySlug lists each project's issues in list order.
var issuesBySlug = map[string][]Issue{
	"billing": {
		{
			Number: 31, Title: "Proration credit drops the last day of a mid-month plan change",
			Status: statusOpen, Priority: prioHigh,
			Assignee: "Mina Fischer", Reporter: "Alba Cruz",
			Created: "02 Sep", Updated: "19 Sep",
			Paragraphs: []string{
				"When a customer switches plans on the 30th or 31st, the proration engine credits every day of the old plan except the final one. Finance reconciles by hand at month end, about forty accounts last cycle.",
				"The root cause is the day-count math in prorate.go: it computes the billed span with an exclusive end date, while the ledger writer assumes it is inclusive. The two functions were written a year apart and never agreed.",
				"Repro: change any monthly plan on the last day of a 31-day month with the API, then diff the credit note against the invoice lines.",
			},
			Comments: []Comment{
				{Author: "Alba Cruz", On: "03 Sep", Text: "Reproduced on staging with the seed account AC-2201. The credit note is exactly one day-rate short."},
				{Author: "Mina Fischer", On: "19 Sep", Text: "Fix is up for review. I added the missing day to the span and a reconciliation test that fails without it."},
			},
		},
		{
			Number: 42, Title: "Retry card updates when the payment service provider times out",
			Status: statusProgress, Priority: prioUrgent,
			Assignee: "Otis Vance", Reporter: "Mina Fischer",
			Created: "29 Aug", Updated: "22 Sep",
			Paragraphs: []string{
				"When the PSP accepts a connection but never answers the status call, the update worker marks the card update failed and moves on. Customers see a failure that actually succeeded on the PSP side, and the next charge double-bills them.",
				"We need idempotent retries keyed on the update token: on an ambiguous outcome, poll the PSP status endpoint with backoff for up to five minutes before declaring failure.",
				"Support has handled eleven double-charge tickets this month. Each refund costs us the original transaction fee, so this is money, not just noise.",
			},
			Comments: []Comment{
				{Author: "Otis Vance", On: "30 Aug", Text: "Ambiguous-outcome polling is in. Working on the ledger guard so a late success after a declared failure flips the record instead of duplicating it."},
				{Author: "Mina Fischer", On: "12 Sep", Text: "Please land the ledger guard first — the poller alone makes the window smaller but not closed."},
				{Author: "Otis Vance", On: "22 Sep", Text: "Both halves merged behind the psp-idempotency flag. Watching the staging ledger for a week before we open it up."},
			},
		},
		{
			Number: 57, Title: "Invoice PDF repeats the customer VAT number twice",
			Status: statusOpen, Priority: prioLow,
			Assignee: "Theo Ito", Reporter: "Jonas Weber",
			Created: "11 Sep", Updated: "11 Sep",
			Paragraphs: []string{
				"EU invoices print the VAT identification number in both the header block and the totals block. The template was duplicated when we split out the reverse-charge variant and both copies render for regular EU customers.",
				"Accounts in Germany and France have asked whether the document is valid. It is, but it looks careless, and one auditor flagged it in a sample check.",
			},
			Comments: []Comment{
				{Author: "Theo Ito", On: "11 Sep", Text: "One-line template fix plus a golden-file assertion. I'll take it after the statement rewrite lands."},
			},
		},
		{
			Number: 63, Title: "Dunning emails stop after the first reminder",
			Status: statusBlocked, Priority: prioHigh,
			Assignee: "Priya Nandi", Reporter: "Alba Cruz",
			Created: "04 Aug", Updated: "16 Sep",
			Paragraphs: []string{
				"The dunning sequence is supposed to send three reminders over ten days. Since the mail-provider migration in July only the first one goes out; the follow-ups are queued and then silently expire.",
				"The migration renamed the sequence handle, so the scheduler queues reminders under a handle the sender no longer subscribes to. Nothing errors, which is why it took a customer telling us.",
				"Blocked on the mail provider: they need to confirm the old handle can be aliased rather than replayed, because a replay would re-send reminders to customers who already paid.",
			},
			Comments: []Comment{
				{Author: "Priya Nandi", On: "16 Sep", Text: "Provider call is Thursday. If the alias is off the table I'll write a one-off replay with a paid-state filter and run it past finance."},
				// The one malformed record: the July migration wrote this
				// event with the provider's internal handle in the
				// author field and no timestamp. The loader panics on it
				// (a data bug on this one issue), the boundary answers.
				{Author: "seq\u2044dunning@provider", On: "", Text: "", Malformed: true},
			},
		},
		{
			Number: 88, Title: "Add a sandbox toggle for test-mode payment methods",
			Status: statusOpen, Priority: prioMedium,
			Assignee: "Theo Ito", Reporter: "Otis Vance",
			Created: "18 Sep", Updated: "18 Sep",
			Paragraphs: []string{
				"QA needs to exercise the checkout flow with the PSP's test cards, but the dashboard hides test-mode payment methods entirely. Every verification round currently needs a database edit on staging.",
				"A per-workspace sandbox toggle in billing settings would show test methods, badge them clearly, and keep them out of live charges and exports.",
			},
		},
		{
			Number: 104, Title: "Currency rounding differs between the quote and the charge",
			Status: statusDone, Priority: prioUrgent,
			Assignee: "Mina Fischer", Reporter: "Jonas Weber",
			Created: "21 Aug", Updated: "09 Sep",
			Paragraphs: []string{
				"Quotes round half-up to the minor unit; the charge path rounds half-even. On currencies with two-decimal minor units the two agree, but on zero-decimal currencies like JPY, and three-decimal ones like KWD, a quote can be one unit off the charge.",
				"Standardised both paths on half-even with explicit banker's-rounding tests for JPY, KWD, and CLP. Finance signed off on the semantics; the difference was within their materiality threshold either way.",
			},
			Comments: []Comment{
				{Author: "Mina Fischer", On: "09 Sep", Text: "Shipped in 2.141.0. The fix is behind the same flag as the proration work so we can back it out in one move if reconciliation objects."},
			},
		},
		{
			Number: 119, Title: "Subscription pause should keep seat-based add-ons billable",
			Status: statusOpen, Priority: prioMedium,
			Assignee: "Alba Cruz", Reporter: "Priya Nandi",
			Created: "23 Sep", Updated: "24 Sep",
			Paragraphs: []string{
				"Pausing a subscription currently pauses everything, including per-seat add-ons the customer wants to keep paying for during the pause. Two accounts have asked to pause the base plan but keep their support seats active.",
				"The pause record needs a keep-billing list of add-on handles. Product agrees; the open question is what the invoice looks like during a pause, since today we suppress the document entirely.",
			},
		},
		{
			Number: 127, Title: "Migrate the invoice PDF pipeline off the reporting VM",
			Status: statusProgress, Priority: prioMedium,
			Assignee: "Jonas Weber", Reporter: "Mina Fischer",
			Created: "01 Sep", Updated: "23 Sep",
			Paragraphs: []string{
				"The reporting VM that renders every invoice PDF is a single point of failure we have been meaning to retire for two years. It runs a hand-built headless browser from 2021, segfaults under load, and only Reza knows the deploy incantation.",
				"This is the full migration plan as agreed with infrastructure.",
				"Week one: stand up the renderer service on the shared Kubernetes cluster with the same font set. The fonts matter — invoices are legal documents and a missing glyph changes line wrapping, which changes page breaks, which changes the page-count watermark customers' auditors rely on.",
				"Week two: dual-write. Every render request produces a PDF on both paths and a diff job compares them byte-for-byte after normalising creation timestamps. Expect a long tail of one-pixel differences from font hinting; the acceptance bar is textually identical and visually equivalent.",
				"Week three: shadow traffic. Send live render requests to the new path without serving the result, and page the on-call on any error rate above a hundredth of a percent. Reza's VM stays warm the whole time.",
				"Week four: cut over EU customers, which is thirty percent of volume and the strictest audit requirements. Keep the rollback path to the old VM one command away for two weeks.",
				"Week five: cut over the rest. Archive the VM image but do not delete it — two enterprise accounts contractually reference documents that must remain reproducible for seven years.",
				"Security review happens in parallel in week two: the renderer takes untrusted customer templates, so the new sandbox needs the same seccomp profile the old one grew organically, written down this time.",
				"Cost: the new path is expected to be cost-neutral at current volume and cheaper above it, because the VM is sized for the month-end peak and idles the rest of the time.",
				"Risks: font drift (mitigated by pinning and the diff job), template regressions (golden files for every template version), and the creation-timestamp normalisation hiding a real timestamp bug (spot-check by hand on day one).",
				"Success criteria: thirty consecutive days of zero diff-job discrepancies above the noise floor, and the reporting VM powered off at the end of the month.",
				"Out of scope: rendering receipts (they already use the new service) and the HTML preview (it never used the VM).",
			},
			Comments: []Comment{
				{Author: "Jonas Weber", On: "12 Sep", Text: "Renderer service is up. Font set pinned by hash; the diff job caught a kerning difference in the currency table on day two, fixed by matching the freetype version."},
				{Author: "Mina Fischer", On: "23 Sep", Text: "Shadow traffic starts Monday. Reza has the rollback runbook and has agreed to be on call for the EU cut."},
			},
		},
	},
	"search": {
		{
			Number: 8, Title: "Typo tolerance eats valid product SKUs in exact-match queries",
			Status: statusOpen, Priority: prioHigh,
			Assignee: "Priya Nandi", Reporter: "Otis Vance",
			Created: "07 Sep", Updated: "20 Sep",
			Paragraphs: []string{
				"A query in double quotes should be exact, but the fuzzy layer still applies typo tolerance inside quoted phrases. SKUs like WT-4110 match WT-4101 and customers land on the wrong part.",
				"The exact-match flag is parsed and threaded as far as the query tree, then dropped on the floor at the fuzzy rewriter. Threads that were supposed to carry it diverged during the pipeline rewrite.",
			},
			Comments: []Comment{
				{Author: "Priya Nandi", On: "20 Sep", Text: "Found the dropped thread. Fix plus a regression suite of quoted-SKU queries is in review."},
			},
		},
		{
			Number: 15, Title: "Facet counts drift after a reindex",
			Status: statusProgress, Priority: prioMedium,
			Assignee: "Theo Ito", Reporter: "Jonas Weber",
			Created: "25 Aug", Updated: "17 Sep",
			Paragraphs: []string{
				"After every nightly reindex, category facet counts are subtly wrong until a searcher opens each facet once. The counts come from a sidecar that the reindex rebuilds lazily per facet.",
				"Fix is to build the sidecar eagerly at the end of the reindex job. The job gets about four minutes longer, which fits comfortably in the maintenance window.",
			},
		},
		{
			Number: 23, Title: "Ranked results reshuffle when a synonym fires on page two",
			Status: statusOpen, Priority: prioMedium,
			Assignee: "Priya Nandi", Reporter: "Alba Cruz",
			Created: "14 Sep", Updated: "15 Sep",
			Paragraphs: []string{
				"Synonym expansion changes the score of some documents mid-pagination, so the ordering of page two can repeat items from page one. Users notice when they are comparing similar products.",
				"The expansion should be computed once per query and frozen for the session's pagination, not recomputed per page with different corpus statistics.",
			},
		},
		{
			Number: 34, Title: "Empty query string returns yesterday's top sellers instead of nothing",
			Status: statusBlocked, Priority: prioLow,
			Assignee: "Theo Ito", Reporter: "Mina Fischer",
			Created: "02 Sep", Updated: "10 Sep",
			Paragraphs: []string{
				"Clearing the search box navigates to a URL with an empty q, and the pipeline interprets that as a merchandising query for yesterday's bestsellers. It was a deliberate growth experiment in March that nobody remembers opting into.",
				"Blocked on product: merchandising claims the empty-state conversion bump is real and wants an A/B test before we change it. The test needs the experiment platform's new holdout support, which lands next quarter.",
			},
		},
		{
			Number: 41, Title: "Index backpressure drops documents during flash sales",
			Status: statusOpen, Priority: prioUrgent,
			Assignee: "Otis Vance", Reporter: "Jonas Weber",
			Created: "20 Sep", Updated: "21 Sep",
			Paragraphs: []string{
				"During the flash sale rehearsal, inventory updates flooded the indexer and it started dropping document versions under backpressure. Buyers searched for items that had sold out minutes earlier and checked out against stale stock.",
				"The indexer needs a durable queue in front of it rather than an in-memory buffer with a drop policy. The drop policy was a deliberate choice in 2022 to protect latency; the flash-sale volume profile is simply outside its design envelope.",
			},
			Comments: []Comment{
				{Author: "Otis Vance", On: "21 Sep", Text: "Sizing the queue from the rehearsal trace. Peak publish rate was 40x the daily average; a day's worth of headroom fits in one small topic."},
			},
		},
		{
			Number: 52, Title: "Search-as-you-type sends queries before the debounce settles",
			Status: statusDone, Priority: prioMedium,
			Assignee: "Alba Cruz", Reporter: "Theo Ito",
			Created: "12 Aug", Updated: "05 Sep",
			Paragraphs: []string{
				"The autocomplete widget fires a query on every keystroke once the buffer reaches three characters, regardless of the debounce. At a hundred queries per session the pipeline rate-limits the user and autocomplete goes dark for a minute.",
				"Fixed the debounce wiring and added a request sequencer that drops stale responses. Also lowered the minimum characters to two, which product wanted anyway.",
			},
		},
	},
	"auth": {
		{
			Number: 3, Title: "Passkey registration should require a recent sign-in",
			Status: statusOpen, Priority: prioHigh,
			Assignee: "Mina Fischer", Reporter: "Priya Nandi",
			Created: "09 Sep", Updated: "19 Sep",
			Paragraphs: []string{
				"A stolen session cookie can currently register a new passkey for the account, silently taking it over. Registration should demand a fresh authentication — either a passkey assertion or a password check — before it writes credentials.",
				"The step-up needs to be proportional: password users get a password prompt, passkey users get an assertion, and SSO-only accounts fall back to their provider's re-authentication.",
			},
			Comments: []Comment{
				{Author: "Mina Fischer", On: "19 Sep", Text: "Design is written up. The SSO fallback is the only open question — some providers re-auth silently, which defeats the purpose."},
			},
		},
		{
			Number: 9, Title: "Session revocation lags by up to a minute under load",
			Status: statusProgress, Priority: prioUrgent,
			Assignee: "Otis Vance", Reporter: "Mina Fischer",
			Created: "01 Sep", Updated: "18 Sep",
			Paragraphs: []string{
				"Sign-out writes the revocation to the store, but replicas serve the old session record for up to sixty seconds. A signed-out device keeps working through the propagation window.",
				"The fix is a revocation generation on the token itself: sessions carry the generation they were minted under, and any token older than the account's current revocation generation is refused without consulting the store.",
			},
		},
		{
			Number: 17, Title: "Device trust banner shows on every reload for Safari users",
			Status: statusOpen, Priority: prioLow,
			Assignee: "Theo Ito", Reporter: "Alba Cruz",
			Created: "16 Sep", Updated: "17 Sep",
			Paragraphs: []string{
				"The remembered-device cookie is set with SameSite=None, which Safari's intelligent tracking prevention discards after seven days or cross-site navigation. Users on Safari see the verify-your-device banner weekly.",
				"We can fall back to a server-side device registry keyed on a stable hash, trading a little storage for a banner that stays gone.",
			},
		},
		{
			Number: 24, Title: "Add a sign-in history page",
			Status: statusOpen, Priority: prioMedium,
			Assignee: "Alba Cruz", Reporter: "Jonas Weber",
			Created: "22 Sep", Updated: "22 Sep",
			Paragraphs: []string{
				"Support needs somewhere to point users who ask whether someone else got into their account. The events are already recorded for audit; they just have no user-facing surface.",
				"The page should show time, device, approximate location, and outcome, with a notice banner when a new device first appears. Thirty days of retention matches the audit export.",
			},
		},
		{
			Number: 30, Title: "Rate limiter counts failed passkey assertions as successful sign-ins",
			Status: statusDone, Priority: prioHigh,
			Assignee: "Priya Nandi", Reporter: "Mina Fischer",
			Created: "19 Aug", Updated: "08 Sep",
			Paragraphs: []string{
				"The limiter increments its counter before the assertion is verified, so a user fumbling their passkey three times locks the account for fifteen minutes. Legitimate users hit it constantly on touch-ID laptops with dirty sensors.",
				"Moved the increment behind verification and added a separate, much higher counter for assertion failures to keep brute-force protection meaningful.",
			},
		},
		{
			Number: 44, Title: "Email verification links expire before the email arrives",
			Status: statusBlocked, Priority: prioMedium,
			Assignee: "Jonas Weber", Reporter: "Theo Ito",
			Created: "05 Sep", Updated: "13 Sep",
			Paragraphs: []string{
				"Verification tokens live fifteen minutes, but the mail provider's own queue delay on weekday mornings runs twenty to twenty-five. The link is dead on arrival and the error page tells users to request a new one, which lands them in the same queue.",
				"Blocked on the mail provider's queue insight endpoint going live, which is the only honest way to pick a lifetime. Interim mitigation shipped: the error page now deep-links the resend.",
			},
		},
	},
	"legacy": {
		{
			Number: 12, Title: "Nightly export duplicates rows when the job overlaps itself",
			Status: statusOpen, Priority: prioMedium,
			Assignee: "Theo Ito", Reporter: "Jonas Weber",
			Created: "30 Aug", Updated: "30 Aug",
			Paragraphs: []string{
				"When the nightly export runs past midnight it overlaps the next run and both write the same batch window. The enterprise accounts reconcile duplicates by hand every month and have started charging us for the effort.",
				"The overlap window is small but the cron schedule was set for a database half the current size. A simple lock on the batch window would close it.",
			},
		},
		{
			Number: 19, Title: "Timezone of the created_at column flipped during the 2023 migration",
			Status: statusOpen, Priority: prioLow,
			Assignee: "Alba Cruz", Reporter: "Priya Nandi",
			Created: "13 Sep", Updated: "13 Sep",
			Paragraphs: []string{
				"Records created before the 2023 platform migration store created_at in local server time; after it, UTC. Any report spanning the boundary is off by up to two hours depending on the month.",
				"We have a conversion table, but reports are generated by the accounts themselves and they cannot be asked to know about our migration. The fix is a one-time backfill plus a view that presents a single timezone.",
			},
		},
		{
			Number: 26, Title: "SOAP endpoint rejects passwords containing ampersands",
			Status: statusDone, Priority: prioMedium,
			Assignee: "Theo Ito", Reporter: "Alba Cruz",
			Created: "18 Aug", Updated: "01 Sep",
			Paragraphs: []string{
				"The XML builder concatenates the password into the envelope without escaping. Anyone with an & in their password fails authentication with a parse error on our side.",
				"Escaped the five XML entities at the one insertion point. The endpoint is scheduled to retire when the last enterprise account moves, which keeps being next quarter.",
			},
		},
		{
			Number: 33, Title: "Audit log purge deletes legal-hold records",
			Status: statusOpen, Priority: prioUrgent,
			Assignee: "Mina Fischer", Reporter: "Jonas Weber",
			Created: "21 Sep", Updated: "24 Sep",
			Paragraphs: []string{
				"The ninety-day purge job has no concept of a legal hold flag because the flag postdates it. Two records under an active dispute were deleted last week; the archive had them, but the production log is what the opposing counsel asked for.",
				"The purge needs to join against the hold table before deleting anything, and the deletion itself should move records to the archive rather than drop them.",
			},
			Comments: []Comment{
				{Author: "Mina Fischer", On: "24 Sep", Text: "Hold join is written. Archive-first deletion needs a capacity check with infrastructure — the archive volume estimate triples."},
			},
		},
		{
			Number: 40, Title: "Customer portal renders a stack trace on database connection loss",
			Status: statusOpen, Priority: prioHigh,
			Assignee: "Otis Vance", Reporter: "Mina Fischer",
			Created: "08 Sep", Updated: "09 Sep",
			Paragraphs: []string{
				"When the connection pool empties, the portal template renders the exception object itself. The trace includes the connection string with credentials, on a page that needs no sign-in.",
				"The global error handler was never wired for the portal's controller hierarchy because the portal predates it. Wiring it up is an afternoon; scheduling the maintenance window on the legacy platform is the hard part.",
			},
		},
	},
}

// allIssues flattens the model for counting and the recent-activity feed.
func allIssues() []Issue {
	var out []Issue
	for _, p := range projects {
		out = append(out, issuesBySlug[p.Slug]...)
	}
	return out
}

// countByStatus counts issues by status across everything.
func countByStatus(status string) int {
	n := 0
	for _, iss := range allIssues() {
		if iss.Status == status {
			n++
		}
	}
	return n
}

// countDoneThisWeek counts issues updated to Done recently. The model's
// dates are display strings, so recency is the two most recent updated
// stamps among Done issues — stable, honest for a fixture, and derived
// rather than hard-written.
func countDoneThisWeek() int {
	var stamps []string
	for _, iss := range allIssues() {
		if iss.Status == statusDone {
			stamps = append(stamps, iss.Updated)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(stamps)))
	seen, week := 0, ""
	for i, s := range stamps {
		if i == 0 {
			week = monthOf(s)
		}
		if monthOf(s) == week {
			seen++
		}
	}
	return seen
}

// monthOf extracts the month token from a "19 Sep" stamp.
func monthOf(stamp string) string {
	parts := strings.Fields(stamp)
	if len(parts) < 2 {
		return stamp
	}
	return parts[1]
}

// openCount counts a project's unresolved issues.
func openCount(slug string) int {
	n := 0
	for _, iss := range issuesBySlug[slug] {
		if iss.Status != statusDone {
			n++
		}
	}
	return n
}

// statusCounts counts a project's issues per status, in chart order.
func statusCounts(slug string) (open, inProgress, blocked, done int) {
	for _, iss := range issuesBySlug[slug] {
		switch iss.Status {
		case statusOpen:
			open++
		case statusProgress:
			inProgress++
		case statusBlocked:
			blocked++
		case statusDone:
			done++
		}
	}
	return
}

// FeedEvent is one entry on a cross-issue activity feed (the home aside
// and the bell popover).
type FeedEvent struct {
	Author   string
	On       string
	Summary  string
	Detail   string
	IssueNum int
}

// recentEvents assembles the latest activity across projects, newest
// first, keyed off the issue comments (the feed's real source).
func recentEvents(limit int) []FeedEvent {
	var out []FeedEvent
	for _, iss := range allIssues() {
		for _, c := range iss.Comments {
			out = append(out, FeedEvent{
				Author: c.Author, On: c.On,
				Summary:  issueKey(iss.Number) + " · " + iss.Title,
				Detail:   c.Text,
				IssueNum: iss.Number,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].On > out[j].On })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// issueByNumber resolves an issue within a project slug.
func issueByNumber(slug string, n int) (Issue, bool) {
	for _, iss := range issuesBySlug[slug] {
		if iss.Number == n {
			return iss, true
		}
	}
	return Issue{}, false
}

// notification is one bell/inbox entry.
type notification struct {
	Author string
	On     string
	Text   string
	Unread bool
	Num    int // issue it belongs to, 0 = none
}

var notifications = []notification{
	{Author: "Mina Fischer", On: "24 Sep", Text: "commented on LEG-33: the hold join is written, archive-first deletion needs a capacity check.", Unread: true, Num: 33},
	{Author: "Otis Vance", On: "22 Sep", Text: "commented on BIL-42: both halves merged behind the psp-idempotency flag.", Unread: true, Num: 42},
	{Author: "Priya Nandi", On: "20 Sep", Text: "commented on SRCH-8: quoted-SKU regression suite is in review.", Unread: true, Num: 8},
	{Author: "Jonas Weber", On: "23 Sep", Text: "moved BIL-127 to In progress: shadow traffic starts Monday.", Unread: false, Num: 127},
	{Author: "Alba Cruz", On: "17 Sep", Text: "reported AUTH-17: Safari users see the device banner weekly.", Unread: false, Num: 17},
}

// unreadNotificationCount feeds the bell badge.
func unreadNotificationCount() int {
	n := 0
	for _, nt := range notifications {
		if nt.Unread {
			n++
		}
	}
	return n
}
