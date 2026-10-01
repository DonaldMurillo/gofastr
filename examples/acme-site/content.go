package main

// The site's content: the help center's articles, the changelog's
// releases, the pricing plans. Real copy about the tracker product the
// marketing site sells — no lorem, no placeholders.

// helpBlock is one block of an article's body. Exactly one field
// family is set per block: heading (+ level), para, bullets, note (an
// info callout), or shortcuts (chord + what it does).
type helpBlock struct {
	heading   string
	level     int // with heading: 2 or 3; 0 takes 2
	para      string
	bullets   []string
	note      string
	shortcuts [][2]string
}

// HelpArticle is one help center article.
type HelpArticle struct {
	Slug    string
	Title   string
	Summary string
	Blocks  []helpBlock
}

// helpArticles is the help center, in reading order — the order the
// pager follows and the nav lists. The glossary deliberately has no
// headings: it is the short article whose empty toc outlet collapses
// the rail.
var helpArticles = []HelpArticle{
	{
		Slug:    "projects",
		Title:   "Organizing work into projects",
		Summary: "Projects are the top of the tracker: every issue, filter and report lives inside one.",
		Blocks: []helpBlock{
			{para: "A project holds its own issues, its own activity feed and its own health badge. The sidebar lists every project your team works, and the count beside each name is the number of issues still open there. The Overview page carries a card per project with the same count and the project's opened-per-week trend."},
			{heading: "Creating a project", level: 2},
			{para: "Projects come from the workspace settings, not from the sidebar: a name, a short slug, and the people who work it. The slug becomes the project's URL — /projects/billing — and never changes after creation, so links to issues keep working for as long as the project exists."},
			{heading: "Project health", level: 2},
			{para: "Each project carries a health badge — on track, at risk or off track — set by the project's owner and shown everywhere the project appears: the sidebar, the project's own header and the Overview cards. It is a one-glance answer to \"is this one fine?\", nothing more; the issue list is where the truth lives."},
			{heading: "Moving between projects", level: 2},
			{para: "Open a project and its layer arrives whole: the header, the issue list, the detail pane. Moving between one project's issues keeps that layer in place — the list does not reload, its scroll and filter survive. Opening a different project swaps the layer for the new one, and your place in the first project is where you left it when you go back."},
		},
	},
	{
		Slug:    "issues",
		Title:   "Working with issues",
		Summary: "Issues are the unit of work: one number, one title, a status, a priority and an assignee.",
		Blocks: []helpBlock{
			{para: "Everything trackable in Acme Tracker is an issue. An issue belongs to exactly one project, carries a number that is assigned once and never reused, and lives at a URL of its own you can paste anywhere."},
			{heading: "Creating an issue", level: 2},
			{para: "New issue sits in a project's toolbar. An issue takes a title, a priority and an assignee; the number comes from the project. BIL-42 is the forty-second issue of the Billing project forever — closing it does not free the number, and no other issue will ever wear it."},
			{heading: "Statuses and priorities", level: 2},
			{bullets: []string{
				"Open — nothing is happening yet; the issue is triaged and waiting.",
				"In progress — someone has taken it; the assignee's avatar leads the row.",
				"In review — the work is done and a second pair of eyes is on it.",
				"Done — closed. Done issues stay in the list and in the history.",
			}},
			{para: "Priority is the crowd-control layer on top of status: low, medium, high and urgent. It colours the tag beside the issue's key and sorts nothing by itself — the list's order is yours to filter."},
			{heading: "Assigning work", level: 2},
			{para: "Assign from the issue's toolbar; the assignee's avatar and name show in the issue's header and in the list row. An issue can sit unassigned with its reporter's name on it until someone takes it."},
			{heading: "Reading an issue", level: 2},
			{para: "An issue page is its own URL — /projects/billing/issues/42 — safe to paste into a chat, a commit message or a review. On a wide screen the project's list stays beside the issue, so the next issue is one click away; on a phone the issue owns the whole page and a back link returns to the project's list."},
		},
	},
	{
		Slug:    "filters",
		Title:   "Filtering and saved views",
		Summary: "Apply a filter to narrow a project's issue list.",
		Blocks: []helpBlock{
			{para: "Every project's list carries a filter box. Enter an issue key, title, status or assignee, then choose Apply. The server returns matching issues and records the filter in the URL. Moving between those issues keeps the filtered list and its scroll position."},
			{heading: "Saved views", level: 2},
			{para: "A filter you keep retyping can be saved under a name — \"Urgent and unassigned\", \"This week's review queue\" — and reopened from the list's toolbar in one click. Saved views belong to the project they were built in; a view is one project's question about itself."},
			{heading: "Pinned views", level: 2},
			{para: "Pin a saved view and it travels with you: moving between the project's issues keeps the pinned filter applied to the list, so a triage session stays a triage session. Unpin and the list returns to showing everything."},
			{note: "Saved views and pinned views arrived in Acme Tracker 2.4. The changelog page carries the full release notes."},
		},
	},
	{
		Slug:    "notifications",
		Title:   "Notifications and the inbox",
		Summary: "The inbox collects what concerns you; the bell keeps the count; digests keep the noise down.",
		Blocks: []helpBlock{
			{para: "The inbox is yours alone: one list of the events that concern you, newest first, unread in bold. It is the tracker's answer to \"did anything need me?\" — nothing arrives there that does not."},
			{heading: "What lands in the inbox", level: 2},
			{bullets: []string{
				"Mentions — someone wrote your name in a comment or a description.",
				"Assignments — an issue was assigned to you, or handed to you from someone else.",
				"Status changes on your issues — the issues you reported or are assigned moving on without you.",
				"Nothing else. Watchers and CC fields do not exist; if you need to know, you will be named.",
			}},
			{heading: "Unread counts", level: 2},
			{para: "The bell in the top bar keeps the unread count and opens a popover with the latest entries — author, one line, the time. Reading an entry in the popover marks it read; the inbox is the full list when you want history."},
			{heading: "Digests", level: 2},
			{para: "If a day's notifications would arrive faster than you would read them, the inbox can fold them: one entry per issue per day, expanded in place. Digests arrived in 2.1 and are off by default — the inbox is a list, not a firehose, on purpose."},
		},
	},
	{
		Slug:    "glossary",
		Title:   "Words we use",
		Summary: "The four words the rest of the help center leans on, in two short paragraphs.",
		Blocks: []helpBlock{
			{para: "A project is the container — one product line, one client, one ongoing effort — and it owns its issues, its activity and its health badge. An issue is the unit of work inside a project: numbered once, never renumbered, and always at a URL of its own."},
			{para: "A view is a filter over one project's list, saved under a name when you reuse it and pinned when you want it to travel with you. The inbox is the only thing in the tracker that is yours alone; every other list is shared with the team as it stands."},
		},
	},
	{
		Slug:    "keyboard",
		Title:   "Keyboard shortcuts",
		Summary: "The tracker is drivable from the keyboard alone: search, filter, and walk an issue list without touching the mouse.",
		Blocks: []helpBlock{
			{para: "Every shortcut works on every page, no mode to enter and no extension to install. The chords are listed beside the places they act on, and the two lists below are the whole set."},
			{heading: "Everywhere", level: 2},
			{shortcuts: [][2]string{
				{"Mod+K", "Open the global search, focused, ready to type."},
				{"/", "Focus the filter box of the list you are looking at."},
				{"Esc", "Close the topmost thing: a menu, a popover, a drawer."},
				{"Shift+Tab", "Step back out of the region you are in."},
			}},
			{heading: "On an issue", level: 2},
			{shortcuts: [][2]string{
				{"j", "Move to the next issue in the list."},
				{"k", "Move to the previous issue in the list."},
				{"e", "Assign the issue (opens the people picker)."},
				{"c", "Change the issue's status."},
			}},
			{note: "Shortcuts respect focus: nothing fires while you are typing in a field. The full list also lives in the product's own shortcut sheet, one Mod+/ away."},
		},
	},
	{
		Slug:    "archived-projects",
		Title:   "Archived projects",
		Summary: "Archiving keeps a project readable forever while taking it out of the daily list.",
		Blocks: []helpBlock{
			{para: "A finished project is not a deleted project. Archiving takes the project out of the sidebar and the Overview cards but keeps every issue, every comment and every event exactly where it was — the work stays readable, it just stops asking to be worked."},
			{heading: "What archiving keeps", level: 2},
			{bullets: []string{
				"Every issue at its own URL, statuses and priorities as they were the day the project closed.",
				"The full activity feed, from the first event to the last.",
				"Search: an archived project's issues still answer the global search.",
				"The project's slug. /projects/legacy was, is and remains /projects/legacy.",
			}},
			{heading: "Reading old activity", level: 2},
			{para: "An archived project's activity is read only in the old system: the issue page says so beside the feed instead of showing one, and nothing in an archived project can be assigned, moved or closed. The archive is a record, not a second inbox."},
			{heading: "Unarchiving", level: 2},
			{para: "A workspace owner can unarchive a project from its own page; it returns to the sidebar with everything intact, and the activity picks up from the moment it left off. Archiving is reversible; deleting is what is not — and deleting asks twice."},
		},
	},
}

// articleBySlug finds an article by its slug.
func articleBySlug(slug string) (HelpArticle, bool) {
	for _, a := range helpArticles {
		if a.Slug == slug {
			return a, true
		}
	}
	return HelpArticle{}, false
}

// release is one changelog entry.
type release struct {
	Version string
	Date    string
	ID      string // the section anchor the announcement links to
	Items   []string
}

// releases is the changelog, newest first.
var releases = []release{
	{
		Version: "2.4", Date: "September 24, 2026", ID: "v2-4",
		Items: []string{
			"Pinned views — pin a saved view and it travels with you between a project's pages; the list keeps the filter across issues.",
			"Saved views — name a filter once and reopen it from the list's toolbar; a view belongs to the project it was built in.",
		},
	},
	{
		Version: "2.3", Date: "September 2, 2026", ID: "v2-3",
		Items: []string{
			"Keyboard shortcuts everywhere — search, filter and walk an issue list without the mouse; the help center carries the full list.",
			"An issue's activity arrives as its own request beside the page, so the issue itself paints first and the feed streams in after.",
			"The bell's popover keeps the latest notifications with unread marks, so a glance replaces a trip to the inbox.",
		},
	},
	{
		Version: "2.2", Date: "August 18, 2026", ID: "v2-2",
		Items: []string{
			"Archived projects — a finished project leaves the sidebar but keeps every issue, comment and event at the same URLs.",
			"Per-project health badges on the Overview cards, beside the open count and the opened-per-week trend.",
		},
	},
	{
		Version: "2.1", Date: "August 4, 2026", ID: "v2-1",
		Items: []string{
			"The inbox — mentions, assignments and status changes on your issues, newest first, unread in bold.",
			"Notification digests — one entry per issue per day when the day gets loud; off by default.",
		},
	},
	{
		Version: "2.0", Date: "July 15, 2026", ID: "v2-0",
		Items: []string{
			"The new issue pane — a project's list and its issues share one page; every issue has a URL, and Back works because the layers are real.",
			"View transitions between issues that respect reduced-motion settings and switch themselves off when the motion budget says so.",
		},
	},
}

// plan is one pricing tier.
type plan struct {
	Name, Price, Period, Description string
	Features                         []string
	CTALabel, CTAHref                string
	Featured                         bool
}

// plans is the pricing page, in display order.
var plans = []plan{
	{
		Name:        "Starter",
		Price:       "$0",
		Period:      "forever",
		Description: "For one project and the people sharing it.",
		Features: []string{
			"1 project, unlimited issues",
			"The filter box",
			"Community help through this center",
		},
		CTALabel: "Set up your first project",
		CTAHref:  "/help/projects",
	},
	{
		Name:        "Team",
		Price:       "$8",
		Period:      "per user / month",
		Description: "For the team that lives in the tracker.",
		Features: []string{
			"Unlimited projects",
			"Saved and pinned views",
			"The activity timeline, per issue",
			"Keyboard shortcuts",
			"CSV export",
		},
		CTALabel: "See what ships in it",
		CTAHref:  "/changelog",
		Featured: true,
	},
	{
		Name:        "Enterprise",
		Price:       "Talk to us",
		Description: "For the organization that needs SSO and a retention story.",
		Features: []string{
			"Everything in Team",
			"Single sign-on",
			"Audit log",
			"Archived-project retention, in writing",
		},
		CTALabel: "Read about archived projects",
		CTAHref:  "/help/archived-projects",
	},
}
