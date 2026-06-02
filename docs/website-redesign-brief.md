# GoFastr — Website Design Brief

You are designing **the public website for GoFastr**. Not a redesign of the
dogfood example app at `examples/site` — that's a feature gallery, not a
product website. Throw it out as a reference. Start clean.

The example app lives at `examples/site` (a feature showcase for contributors).
What you're designing is the **front door of the project** — what shows up at,
say, `gofastr.dev` — for engineers landing on it for the first time.

Read this whole brief before drawing anything.

---

## 1. The work, in one sentence

Design a real product website for GoFastr from scratch — sitemap, IA, visual
identity, voice, hero, every page — and produce enough detail that a
developer (or another Claude) could implement it inside the GoFastr framework
itself.

---

## 2. What GoFastr actually is

Pre-alpha Go full-stack framework whose central conviction is that **AI
agents are first-class authors of web applications**. Most frameworks assume
a human hand-writes every route, query, validator, migration, and form.
GoFastr inverts that: you (or an agent) declare your domain once, the
framework generates every surface around it.

**The one-declaration-many-surfaces story:**

One `entities/posts.json` (or one `app.Entity(...)` call in Go) produces:

- SQL table + versioned migrations (SQLite or Postgres)
- REST endpoints (`GET / POST /posts`, `/posts/{id}`, batch, SSE stream)
- Filter/sort/pagination DSL (`?status=published&views_gte=10&sort=-created_at`)
- Cursor + offset pagination, eager loading (`?include=author.profile`)
- OpenAPI 3 spec + Swagger UI
- **MCP tools** (`posts_list`, `posts_get`, `posts_create`, `posts_update`,
  `posts_delete`) — so an AI agent can drive the app through the same
  surface humans use
- Typed Go model
- Lifecycle hooks (`BeforeCreate`, `AfterUpdate`, …) that share the parent
  transaction
- Optional soft-delete, multi-tenancy, audit log, file uploads

**The architecture is two layers:**

- `core/` — twelve **stdlib-only** primitives: `router`, `handler`,
  `middleware`, `query`, `mcp`, `schema`, `migrate`, `render`, `static`,
  `upload`, `stream`, `openapi`. Each works standalone.
- `framework/` — the opinionated entity system composed on top.

If the framework is in your way, **you drop down to core**. No reflection
magic. Generated code lives in `.gofastr/entities/` and is normal Go you
can read, debug, and edit.

**The UI story is server-driven:**

`core-ui/` is a separate runtime: signals, HTML primitives, composed
patterns (accordion, tabs, modal, drawer, popover, toast), islands, SSE
streaming, and a ~10 KB gzipped vanilla-JS runtime that hydrates
progressively after first paint. **No React, no client framework.**
Cross-page navigation is client-side with a cache; in-page state changes
are islands; server-pushed updates flow through SSE.

**Batteries are pluggable, not embedded:**

`battery/auth`, `cache`, `email`, `embed` (local semantic search),
`notify`, `queue`, `search`, `storage`, `webhook`, `log`, `admin`. Each
sits behind a small interface with an in-memory implementation for tests
and small apps; production swaps in Redis, S3, Postgres FTS, SES, etc.
Swap one without forking.

**Kiln is the agent-driven build mode:**

A separate binary. Run `kiln serve --agent claude-code` and chat with an
agent in a floating panel; the agent calls Kiln's typed tool surface
(`add_entity`, `add_field`, `add_hook`, `add_page`, `propose_plan`,
`approve_plan` …) over HTTP or MCP; the in-memory IR mutates; the schema
migrates; the running app re-renders — all in-process. Freeze the journal
when done to emit canonical `entities/*.json` you commit. Destructive ops
require an approved plan; agents cannot drop tables without human
confirmation in the UI.

**The CLI surface:**

```
gofastr init <name>        gofastr build       gofastr migrate up|down|status
gofastr generate           gofastr dev         gofastr embed index|watch|query
gofastr theme init         gofastr test        gofastr docs
```

**Honesty:** pre-alpha. APIs change between commits. `core-ui/` is the
active research frontier. The project explicitly says: "Use it to learn,
not to ship customer code." That honesty is part of the brand.

---

## 3. The repository, at a glance

So you can feel the surface area without scrolling:

- 12 `core/` packages (stdlib only)
- ~25 `framework/` packages (entity, crud, filter, dsl, hook, migrate,
  tenant, access, file, cron, event, log, etc.)
- 12 `battery/` packages (auth, cache, embed, log, notify, etc.)
- `core-ui/` with ~30 composed component packages + 30+ runtime modules
- `framework/ui/` with semantic components (PageHeader, StatCard,
  DataTable, FormField, Notification, etc.)
- `kiln/` agent-driven build mode + MCP/ACP servers + chat panel
- 50+ feature docs in `framework/docs/content/*.md`, embedded into the
  binary at build time and browsable with `gofastr docs`
- 6 example apps in `examples/`: website (full feature gallery), blog
  (JSON-declared entities), api-tour, embed-demo, spa (Vue + GoFastr
  API), static-site

The website needs to feel like the front door of all of that — without
overwhelming a first-time visitor.

---

## 4. Who shows up

Primary audience — **Go developers using AI agents to build real apps:**

- Mid-to-senior backend engineers. Comfortable in `database/sql`,
  `net/http`, channels. Tired of writing the same CRUD scaffolding for
  the fourth time.
- Already pair with Claude / Cursor / Copilot for daily work. Want a
  framework that meets the agent halfway instead of fighting it.
- Skeptical of magic. Will read the generated code before trusting it.
- Allergic to client-side complexity. Notice that this site uses no
  React and that's part of the appeal.

Secondary — **agentic-coding enthusiasts:**

- People excited about Kiln-style "describe an app, watch it build."
- People building their own AI dev tooling who want a framework with an
  MCP-first posture.

Tertiary — **Go ecosystem at large:**

- Phoenix/LiveView refugees curious about a Go equivalent.
- Folks evaluating "modern Go web framework" options.

**Explicitly not the audience:**

- Enterprise procurement teams. (Pre-alpha. Wrong fit.)
- No-code visitors expecting a Webflow-style drag-and-drop.
- Beginners learning to program. (This isn't "Rails for newbies.")

Design for the primary. The site should signal "this is for serious Go
people who take AI seriously" within the first 5 seconds.

---

## 5. Personality you need to derive

Don't pick a vibe from a Pinterest board. Read the project, then propose
a personality that's *earned* by what's actually here. The signals in the
source code and docs are:

**Voice — from the existing agent-notes file (excerpt):**

> The `i++` bug. First implementation inserted a fusion-guard space
> whenever two `+` would be adjacent. That broke `i++` into `i+ +`, which
> Acorn rejected as a parse error. Fix: only emit the fusion-guard space
> when the original source had whitespace between the two tokens.

That's the voice. Engineer-to-engineer. Names the bug, names the cause,
names the fix. No "leveraging," no "empowering," no "synergize." Direct,
self-aware, technically dense. **The site should sound like this.**

**Convictions visible in the code:**

- "Declare once, generate many surfaces."
- "No reflection magic. Read the generated code."
- "Drop down to core when the framework is in your way."
- "Batteries included, **not embedded.**"
- "Pre-alpha research. Use it to learn, not to ship."
- "AI agents are first-class authors."

**Tensions to hold:**

- Confident in the architecture **and** transparent about being pre-alpha.
- Pro-AI **without** breathless hype. Not "AI builds your app in
  seconds!" — more "the agent and the engineer build the app together
  and the framework respects both."
- Technical density **without** being a wall of text. The audience reads
  for a living; reward them with depth, but earn the depth with
  signposting and breathing room.
- Strong opinions **without** being smug. The project says no to things,
  but it's pre-alpha and learning in public.

From these, propose **2–3 distinct personality directions** with names,
voice samples, and visual moodboards, **before locking one.** Show your
work. Let the reader (and the human approving this) pick.

---

## 6. What to design (full scope)

A real product website, not just a homepage. At minimum:

1. **Home** — the front door. Must answer in ~5 seconds: *What is this?
   Who is it for? Why should I care?* Hero, the one-declaration-many-
   surfaces story, an example that's actually persuasive, a path forward.
2. **Get started** — install, scaffold, first entity, first page. The
   shortest possible "I have something running" loop. Probably a single
   long-form page, not a multi-step wizard. Show code samples that
   actually work end-to-end.
3. **Concepts** — IA for the 50+ feature docs. Don't just list them
   alphabetically (the current demo does this and it's awful). Group by
   user intent: *Modeling your domain*, *Serving HTTP*, *Building UI*,
   *Operations*, *Working with agents*, etc.
4. **UI showcase** — the framework ships a full UI system. Show it off
   *as the site itself* — every interactive element on the site is one
   of the components. Include a dedicated gallery for browsing.
5. **Kiln** — distinct enough to deserve its own subsite or section.
   This is the "watch an agent build your app live" demo. The personality
   here can be louder than the rest of the site. A video, an interactive
   embedded panel, a transcript that scrolls — pick the format that
   actually persuades.
6. **Examples** — the 6 reference apps. Each gets a card with the
   problem it solves, the entity declaration, a screenshot, and a
   one-line `go run` command.
7. **Philosophy / About** — the two-layer architecture, the convictions,
   the pre-alpha disclosure, the roadmap link. This is where the
   personality is most explicit.
8. **Reference** — auto-generated API docs (this might just link out to
   pkg.go.dev for now). Note in the brief that this exists but isn't
   the design priority.

**Design system you must produce:**

- Typography scale (display, headings, body, code, caption).
- Color tokens (light + dark; the framework's theme system supports
  both — the site must too).
- Spacing scale.
- Motion principles (one or two — not "we use 12 different easings").
- Iconography approach (custom? Lucide? heroicons? — pick one and
  defend it).
- Code-sample component (this is the heart of the site; treat it as a
  hero component with line numbers, tab switching for JSON-vs-Go forms,
  copy button, syntax theme that works in both modes).
- Navigation chrome (mobile + desktop).
- Doc page chrome (TOC, breadcrumbs, prev/next, edit link).

---

## 7. Hard constraints (non-negotiable)

The site is **dogfood**. Built with GoFastr itself. Which means:

- **SSR-first.** Every page fully server-rendered on initial load.
- **Hydration, not re-render.** A vanilla-JS runtime attaches handlers
  to the existing DOM after first paint.
- **Cross-page nav is client-side with cache.** No hard refreshes
  between routes.
- **In-page state changes are islands.** A click on "next page" fires an
  RPC, the server returns new island HTML, the runtime swaps just that
  island. No `location.href = …`. No client-side pagination math.
- **Server-pushed updates flow through signals + SSE** (for live
  metrics, agent chat streams, etc.).
- **Strict CSP by default** — no inline `<script>` or `<style>`. Every
  interactive bit ships as a registered component.
- **Accessible.** WCAG AA contrast, keyboard-navigable, screen-reader
  announceable. The framework's components target this and the site
  must honor it.
- **Works without JS.** First paint is real HTML. JS is enhancement,
  not foundation.
- **Theme tokens, not hardcoded colors.** Use the framework's typed
  theme so light/dark switches cleanly.
- **Mobile-first.** 320px viewport must work. The site should look
  *intentionally* good on a phone, not just "responsive."
- **Performance budget.** Initial JS payload < 25 KB gz. Initial CSS
  < 30 KB gz. LCP target < 1.5s on 4G. Don't ship a hero video that
  ruins this.

The site is also a reference for "what's possible in the framework."
Every pattern you invent for the site should be implementable as a
`core-ui/patterns` package or a `framework/ui` component. If you find
yourself drawing something that can't be cleanly expressed in the
framework's primitives, that's a signal to redesign — or to file a gap
that the framework should fill.

---

## 8. What's open (creative freedom)

Everything not in §7. Specifically:

- Visual identity from scratch. No existing logo to preserve.
- Hero treatment. Could be code-first, terminal-first, narrative-first,
  diagrammatic, interactive — your call. Defend the choice.
- Color & type. Bold or restrained, mono or serif, dark-first or
  light-first — your call.
- Motion language. Stillness can be a choice; so can a lot of motion.
  But pick one and commit.
- Information density. Linear-style "lots of words, tight type" or
  Stripe-style "huge type, generous whitespace" or somewhere else.
- The agent-collaboration angle. How do you visually show "the agent and
  the engineer build together"? Side-by-side panes? Chat-style? A
  diagram? Whatever you pick, it should appear on the home page in
  some form — it's the project's central claim.
- How loud Kiln is. It's the most agent-forward part of the project; it
  may want a different visual register from the rest of the site.

---

## 9. References — yes and no

**Yes — look at these for principle, not pixels:**

- **Linear** — for editorial dev-tool typography and the restraint of a
  product that knows its audience.
- **Astro.build** — for how a framework site treats code samples as
  hero elements.
- **Rauno Freiberg's site (rauno.me)** — for micro-interaction
  discipline.
- **Tailscale's site & blog** — for technical writing as marketing.
- **Mitchell Hashimoto's blog** — for engineer voice without ego.
- **Cloudflare docs** — for IA across hundreds of feature pages.
- **Anthropic's own site** — restrained, confident, not gimmicky.

**No — explicitly avoid:**

- Generic "AI-startup launch page" gradient + 3D-blob aesthetics.
- "Generate <thing> with <tool> in seconds!" hero copy.
- Webflow-template SaaS-landing-page layouts (testimonials carousel,
  pricing tiers, logo cloud — none of that fits this audience).
- Heavy WebGL hero scenes that bloat the bundle.
- Any copy that says "leverage," "empower," "unlock," "revolutionize."
- Stock developer photography (laptops on desks, etc.).
- Cliché terminal mockups with `npm install` when this is a Go project
  and the actual command is `go install ./cmd/gofastr`.

---

## 10. Deliverables

Produce, in order:

1. **A short summary of what you understood** (≤200 words). Mirror back
   the project and the audience so misalignment surfaces before you
   draw.
2. **2–3 personality directions** with names, one-paragraph
   descriptions, voice samples (a real hero headline + subhead written
   in each voice), and a 4–6 image moodboard sketch (described in
   words, not images). Spell out the tradeoffs of each.
3. **One picked direction** (after the human chooses, or — if asked —
   your recommendation with reasoning).
4. **A visual system spec.** Type scale, color tokens (light + dark),
   spacing, motion principles, iconography. Concrete values, not
   "warm and inviting."
5. **A sitemap** with brief notes on each page's job.
6. **Home page detailed design** — described section by section, with
   ASCII-art wireframes or component-level specs. Don't leave it at
   "hero, then features, then CTA."
7. **Get-started page detailed design** — the second-most-important
   page.
8. **Concepts IA proposal** — how the 50+ docs cluster into a
   navigable hierarchy.
9. **Component patterns** specific to this site — code sample block,
   doc page chrome, navigation, the agent-collaboration visualization
   from §8. Concrete enough that a developer could implement them
   inside `core-ui/patterns/`.
10. **A "first paint" plan** — what's on screen in the first 2 seconds.
    What loads after. What's deferred behind an interaction.
11. **A list of open questions** you couldn't answer from this brief
    alone, and what you'd want to know to answer them.

For each deliverable: if a decision is reversible and cheap, make it
and move on. If it's expensive to undo, flag it and ask.

---

## 11. Process notes

- Read the project before designing. The README in `/Users/dom/programming/gofastr/README.md`,
  `framework/ARCHITECTURE.md`, `core-ui/ARCHITECTURE.md`, and
  `framework/docs/content/agent-notes.md` are the ground truth.
- Lo-fi before hi-fi. Words and wireframes before color and type.
- Pick the visual system **before** mocking pages. Pages assembled from
  an undefined system look like every page is a one-off.
- Show your reasoning, not just your conclusion. The most useful part
  of a design brief is often the rejected alternatives.
- The audience cares more about being respected than dazzled. Restraint
  wins ties.

---

## 12. One last thing

The example app at `examples/site` is **not** the inspiration.
It's a feature checklist with chrome around it. It exists to prove the
framework works; it does not exist to make anyone want to *use* the
framework. Your job is the opposite: design something that, after 90
seconds of reading, a senior Go engineer who already uses AI tools
thinks, *"I want to try this on my next side project."*

If you find yourself designing a marketing site, stop. If you find
yourself designing a documentation site, also stop. The right answer
is probably closer to a **technical magazine** — opinionated, written,
visually disciplined, where the code samples are the hero images and the
navigation rewards exploration.

Go.
