package main

// The synthetic dataset. Everything is drawn from one fixed seed, so
// every boot (and every benchmark run) serves the same bytes; nothing
// here is copied from Hacker News. Sizes follow the real front page:
// titles 40–90 characters, a few dozen domains, heavy-tailed points,
// most stories with a handful of comments and a few with hundreds,
// threads that run five and more levels deep.

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"
)

// refTime is "now" for the dataset: every age on every page is
// measured from it, not from the wall clock, so a page renders the same
// bytes on every run.
var refTime = time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)

// Dataset sizes. The list sizes give the HNPWA page counts at 30 per
// page: top 10 pages, newest 12, ask 2, show 2, jobs 1.
const (
	perPage      = 30
	nStory       = 240
	nAsk         = 60
	nShow        = 60
	nJob         = 30
	nUsers       = 700
	topPages     = 10
	seedA, seedB = 0x6e65_7773, 0x6861_636b
)

// dataset is everything the seed writes.
type dataset struct {
	users    []User
	stories  []Story
	comments []CommentRow
}

func generate() dataset {
	r := rand.New(rand.NewPCG(seedA, seedB))
	g := &gen{r: r}
	var d dataset
	d.users = g.users()
	names := make([]string, len(d.users))
	for i, u := range d.users {
		names[i] = u.ID
	}
	g.names = names

	// Story kinds, shuffled into posting order; times walk back from
	// refTime a few minutes apart, so the newest list spans about a day.
	kinds := slices.Concat(repeat("story", nStory), repeat("ask", nAsk), repeat("show", nShow), repeat("job", nJob))
	r.Shuffle(len(kinds), func(i, j int) { kinds[i], kinds[j] = kinds[j], kinds[i] })
	posted := refTime.Add(-time.Duration(r.IntN(120)) * time.Second)
	nextID := 46_100_000
	for _, kind := range kinds {
		posted = posted.Add(-time.Duration(90+r.IntN(300)) * time.Second)
		nextID += 1 + r.IntN(40)
		d.stories = append(d.stories, g.story(strconv.Itoa(nextID), kind, posted))
	}
	// The most-voted stories carry the long threads.
	byPoints := make([]int, len(d.stories))
	for i := range byPoints {
		byPoints[i] = i
	}
	slices.SortFunc(byPoints, func(a, b int) int { return d.stories[b].Points - d.stories[a].Points })
	popular := map[int]bool{}
	for _, i := range byPoints[:18] {
		popular[i] = true
	}
	cid := 46_500_000
	for i := range d.stories {
		s := &d.stories[i]
		if s.Kind == "job" {
			continue
		}
		n := g.commentCount(s.Points, popular[i])
		cs := g.thread(s, n, &cid)
		s.Descendants = len(cs)
		d.comments = append(d.comments, cs...)
	}
	for i := range d.stories {
		d.stories[i].Hot = hotness(d.stories[i])
	}
	return d
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// hotness ranks the top list: votes decayed by age, the classic
// gravity formula. Jobs never rank.
func hotness(s Story) float64 {
	if s.Kind == "job" {
		return 0
	}
	hours := refTime.Sub(time.Unix(s.Posted, 0)).Hours()
	return float64(s.Points-1) / math.Pow(hours+2, 1.8)
}

type gen struct {
	r     *rand.Rand
	names []string
}

func (g *gen) pick(xs []string) string { return xs[g.r.IntN(len(xs))] }

// lognorm draws a heavy-tailed positive number around median.
func (g *gen) lognorm(median, sigma float64) float64 {
	return median * math.Exp(g.r.NormFloat64()*sigma)
}

func (g *gen) users() []User {
	seen := map[string]bool{}
	out := make([]User, 0, nUsers)
	for len(out) < nUsers {
		var name string
		switch g.r.IntN(4) {
		case 0:
			name = g.pick(nameA) + g.pick(nameB)
		case 1:
			name = g.pick(nameA) + "_" + g.pick(nameB)
		case 2:
			name = g.pick(nameB) + strconv.Itoa(g.r.IntN(990)+10)
		default:
			name = g.pick(nameA) + g.pick(nameB) + strconv.Itoa(g.r.IntN(99))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		age := time.Duration(30+g.r.IntN(17*365)) * 24 * time.Hour
		karma := int(g.lognorm(900, 1.6))
		about := ""
		if g.r.IntN(10) < 6 {
			about = g.paragraphs(1+g.r.IntN(2), 1, 3, false)
		}
		out = append(out, User{
			ID: name, Created: refTime.Add(-age).Unix(), Karma: max(karma, 1), About: about,
		})
	}
	return out
}

func (g *gen) story(id, kind string, posted time.Time) Story {
	s := Story{ID: id, Kind: kind, Author: g.pick(g.names), Posted: posted.Unix()}
	s.Points = max(1, int(g.lognorm(28, 1.25)))
	s.Points = min(s.Points, 2400)
	switch kind {
	case "ask":
		s.Title = g.title("Ask HN: ", askTitles)
		s.Text = g.paragraphs(1+g.r.IntN(3), 2, 4, true)
	case "show":
		s.Title = g.title("Show HN: ", showTitles)
		s.Domain = g.pick(domains)
		s.URL = g.url(s.Domain)
		if g.r.IntN(2) == 0 {
			s.Text = g.paragraphs(1+g.r.IntN(2), 2, 4, true)
		}
	case "job":
		s.Title = g.jobTitle()
		s.Domain = g.pick(domains)
		s.URL = g.url(s.Domain)
		s.Points = 0
	default:
		s.Title = g.title("", storyTitles)
		s.Domain = g.pick(domains)
		s.URL = g.url(s.Domain)
	}
	return s
}

// title fills a template until it lands in 40–90 characters.
func (g *gen) title(prefix string, templates []string) string {
	for {
		t := g.fill(g.pick(templates))
		t = prefix + strings.ToUpper(t[:1]) + t[1:]
		if n := len(t); n >= 40 && n <= 90 {
			return t
		}
	}
}

func (g *gen) jobTitle() string {
	for {
		t := fmt.Sprintf("%s %s is hiring %s (%s)", g.pick(companyA), g.pick(companyB), g.pick(roles), g.pick(places))
		if n := len(t); n >= 40 && n <= 90 {
			return t
		}
	}
}

// fill replaces each {slot} in a template from its word list.
func (g *gen) fill(t string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(t, '{')
		if i < 0 {
			b.WriteString(t)
			return b.String()
		}
		j := strings.IndexByte(t[i:], '}')
		b.WriteString(t[:i])
		if name := t[i+1 : i+j]; name == "item" {
			// {item} is a {thing} without its article.
			w := g.pick(slots["thing"])
			b.WriteString(w[strings.IndexByte(w, ' ')+1:])
		} else {
			b.WriteString(g.pick(slots[name]))
		}
		t = t[i+j+1:]
	}
}

func (g *gen) url(domain string) string {
	path := strings.ReplaceAll(g.fill("{item}-{noun}"), " ", "-")
	switch g.r.IntN(4) {
	case 0:
		return "https://" + domain + "/" + strconv.Itoa(2018+g.r.IntN(8)) + "/" + path
	case 1:
		return "https://" + domain + "/posts/" + path
	case 2:
		return "https://" + domain + "/" + path + ".html"
	}
	return "https://" + domain + "/" + path
}

// sentence draws one sentence from the templates.
func (g *gen) sentence() string {
	s := g.fill(g.pick(sentences))
	return strings.ToUpper(s[:1]) + s[1:]
}

// sentences draws k sentences, none repeating the one before it.
func (g *gen) sentences(k int) []string {
	ss := make([]string, k)
	for i := range ss {
		for {
			ss[i] = g.sentence()
			if i == 0 || ss[i] != ss[i-1] {
				break
			}
		}
	}
	return ss
}

// paragraphs joins n paragraphs of lo..hi sentences each with blank
// lines; links adds an occasional URL to a paragraph.
func (g *gen) paragraphs(n, lo, hi int, links bool) string {
	ps := make([]string, n)
	for i := range ps {
		k := lo + g.r.IntN(hi-lo+1)
		p := strings.Join(g.sentences(k), " ")
		if links && g.r.IntN(8) == 0 {
			p += " " + g.url(g.pick(domains))
		}
		ps[i] = p
	}
	return strings.Join(ps, "\n\n")
}

// commentCount sizes a story's thread: most have a few comments, the
// popular ones 80–300.
func (g *gen) commentCount(points int, popular bool) int {
	if popular {
		return 80 + g.r.IntN(221)
	}
	n := int(float64(points) * (0.1 + 0.4*g.r.Float64()))
	return min(n, 45)
}

// thread grows n comments under s: each new comment answers the story
// or, more often, a recent comment, so busy threads run deep.
func (g *gen) thread(s *Story, n int, cid *int) []CommentRow {
	out := make([]CommentRow, 0, n)
	depth := map[string]int{}
	t := time.Unix(s.Posted, 0)
	span := refTime.Sub(t)
	for i := range n {
		*cid += 1 + g.r.IntN(30)
		id := strconv.Itoa(*cid)
		parent := ""
		if i > 0 && g.r.IntN(10) >= 3 {
			// Favour the newest comments: replies cluster on live
			// sub-threads.
			back := min(i, 1+int(math.Abs(g.r.NormFloat64())*6))
			p := out[i-back]
			if depth[p.ID] < 14 {
				parent = p.ID
			}
		}
		if parent != "" {
			depth[id] = depth[parent] + 1
		}
		posted := t.Add(time.Duration(float64(span) * float64(i+1) / float64(n+1)))
		k := 1 + g.r.IntN(6)
		paras := 1
		if k > 2 {
			paras = 1 + g.r.IntN(min(k, 3))
		}
		out = append(out, CommentRow{
			ID: id, StoryID: s.ID, ParentID: parent, Author: g.pick(g.names),
			Posted: posted.Unix(), Body: g.splitBody(k, paras),
		})
	}
	return out
}

// splitBody writes k sentences in paras paragraphs, with an occasional
// link.
func (g *gen) splitBody(k, paras int) string {
	ss := g.sentences(k)
	if g.r.IntN(9) == 0 {
		ss[g.r.IntN(k)] += " " + g.url(g.pick(domains))
	}
	per := (k + paras - 1) / paras
	var ps []string
	for i := 0; i < k; i += per {
		ps = append(ps, strings.Join(ss[i:min(i+per, k)], " "))
	}
	return strings.Join(ps, "\n\n")
}

// Word lists. Invented names and domains; any match with a real site is
// chance.
var (
	nameA = []string{"ada", "bram", "cole", "dara", "eli", "fen", "gus", "hale", "ivo", "jun", "kai", "lena", "mika", "noor", "otto", "pia", "quin", "rhea", "sol", "tove", "uma", "vik", "wren", "xan", "yara", "zed", "bit", "byte", "null", "async", "lisp", "rust", "tcp", "unix", "kern", "vim", "heap", "stack", "loop", "node", "grep", "sed", "awk", "zero", "delta", "sigma", "tau", "nova"}
	nameB = []string{"smith", "wolf", "fox", "hart", "stone", "field", "brook", "lake", "ridge", "frost", "vale", "hacker", "coder", "dev", "ops", "nerd", "fan", "bot", "man", "girl", "dude", "wiz", "punk", "sage", "monk", "pilot", "smith", "works", "labs", "mancer"}

	domains = []string{
		"quillpress.dev", "kernelnotes.org", "byteforge.io", "papers.latticeacademy.org", "blog.northwindlabs.dev",
		"thegridjournal.com", "openfieldnews.com", "codebrook.net", "lowlevel.club", "postmortems.page",
		"slowcompute.org", "eng.harborcloud.io", "fieldgeology.org", "astro-letters.net", "typesystems.blog",
		"tinycircuits.dev", "longformreview.com", "cityplanning.today", "dataduck.dev", "retrocomputing.house",
		"medlettersonline.org", "physicsdigest.net", "seedvault.dev", "infra.mapletech.com", "the-ledger.news",
		"oceanobs.org", "notebook.cs.tinyuniversity.edu", "mathcomments.org", "spectralsound.fm", "economywatch.net",
		"designbrief.studio", "lawandcode.org", "bikeshed.engineering", "garden-of-bits.net", "compilerweekly.com",
		"terminalcraft.dev", "dbinternals.io", "networklore.net", "privacybeat.org", "energyfront.news",
		"spacedispatch.com", "historyofmachines.org", "writing.liminal.ink", "paperclip.science", "startupnotes.vc",
		"queue-theory.net", "browserlab.dev", "typefoundry.works", "frugalrobotics.org", "hardware.hackery.dev",
	}

	storyTitles = []string{
		"{gerund} {thing} with {tech}",
		"Why {thing} is harder than it looks",
		"The {adj} history of {thing}",
		"{org} releases {tech} {version}",
		"How we cut our {item} bill by {pct}",
		"A {adj} guide to building {thing} for {people}",
		"{tech} {version} is out: {feature}",
		"I {pastverb} {thing} in {num} lines of {tech}",
		"{people} are {gerundlow} {thing} again",
		"What {years} years of {gerundlow} {thing} taught me about {noun}",
		"The case against {thing}",
		"{thing}: a {adj} retrospective",
		"Field notes on {noun} from {place}",
		"Understanding {thing} from first principles",
		"Notes on {gerundlow} {thing} at scale",
		"{org} is shutting down its {item} after {years} years",
		"An interactive explanation of {thing}",
		"{thing} in the browser, no server required",
	}
	askTitles = []string{
		"How do you {verb} {thing} at work?",
		"Which {item} do you use, and why?",
		"Is {tech} still worth learning in {year}?",
		"Who is {gerundlow} {thing} on a small team?",
		"Best resources for {gerundlow} {thing}?",
		"How did you get into {field}?",
		"What is your setup for {gerundlow} {thing}?",
		"Anyone else seeing {adj} builds after the {tech} update?",
	}
	showTitles = []string{
		"I built a {adj} {item} in {tech}",
		"{product}, a {adj} {item} for {people}",
		"{product} – {gerundlow} {thing} from your terminal",
		"{thing} that runs entirely in the browser",
		"{product}: an open-source {item} written in {tech}",
		"My weekend project, a {adj} {item}",
	}
	sentences = []string{
		"I {pastverb} this at my last job and the {noun} was the hard part.",
		"the {item} approach works until you need {noun}, then it falls apart.",
		"this matches what we saw when we moved our {item} to {tech}.",
		"I am not sure the benchmark is fair, since the {noun} dominates everything else.",
		"has anyone measured this on real {noun} rather than a toy example?",
		"the article skips over {noun}, which in my experience is where most of the time goes.",
		"we tried {tech} for {thing} and went back after about {num} months.",
		"honestly the {adj} part is the documentation, not the {item} itself.",
		"there is a good talk on {thing} that covers exactly this trade-off.",
		"the real problem is that nobody owns the {noun} once the project ships.",
		"for small teams I would start with {tech} and only change when it hurts.",
		"the {num} percent figure seems high, but the methodology section explains it.",
		"I {pastverb} {thing} like this years ago and still use it every day.",
		"it is worth reading the original paper, it is shorter than you would think.",
		"this is a {adj} write-up, thanks for sharing it.",
		"the comparison would be more useful if it included {tech}.",
		"what worries me is the {noun}, not the {item}.",
		"in {place} this has been normal for years.",
		"you can get most of the benefit with a {adj} {item} and good defaults.",
		"I disagree with the conclusion, but the data in the middle section is great.",
		"the old {item} did this in {num} kilobytes, which says something about where we are.",
		"we hit the same {noun} limit and ended up sharding by customer.",
	}
	slots = map[string][]string{
		"gerund":    {"Building", "Debugging", "Scaling", "Shipping", "Rewriting", "Profiling", "Testing", "Teaching", "Measuring", "Designing"},
		"gerundlow": {"building", "debugging", "scaling", "shipping", "rewriting", "profiling", "testing", "teaching", "measuring", "designing"},
		"thing": {"a garbage collector", "a search index", "an email client", "a type checker", "a text editor", "a time-series database",
			"a static site generator", "a container runtime", "a package manager", "an RSS reader", "a chess engine", "a payroll system",
			"a mapping service", "a rate limiter", "a job queue", "a font renderer", "a tiny operating system", "a password manager",
			"a spreadsheet engine", "a load balancer", "a satellite tracker", "a compiler backend", "a CRDT library", "a logging pipeline"},
		"tech":     {"Go", "Rust", "Zig", "OCaml", "SQLite", "Postgres", "WebAssembly", "Python", "TypeScript", "Erlang", "C", "Haskell", "Lua", "Elixir", "Kotlin"},
		"adj":      {"practical", "short", "surprising", "visual", "complete", "minimal", "brief", "careful", "strange", "slow", "fast", "boring", "small"},
		"org":      {"Northwind Labs", "The Open Data Trust", "Harbor Cloud", "Maple Systems", "Lattice Academy", "The City Archive", "Kestrel Robotics"},
		"version":  {"1.0", "2.0", "3.4", "0.9", "4.1", "10", "2026.1"},
		"pct":      {"40%", "62%", "a third", "half", "90%", "75%"},
		"people":   {"beginners", "backend developers", "data teams", "students", "hobbyists", "small businesses", "researchers", "librarians"},
		"feature":  {"faster builds and a new debugger", "generics and better errors", "a smaller runtime", "first-class async", "a new query planner"},
		"pastverb": {"built", "wrote", "rewrote", "shipped", "debugged", "maintained", "measured", "replaced"},
		"years":    {"three", "five", "seven", "ten", "twelve", "fifteen"},
		"field":    {"compilers", "databases", "embedded work", "security", "distributed systems", "typography", "game development", "open source"},
		"num":      {"three", "five", "seven", "ten", "twelve", "200", "1,000", "15", "40"},
		"noun":     {"latency", "maintenance", "onboarding", "memory use", "build tooling", "test suites", "observability", "hiring", "pricing", "reliability", "security reviews", "data migrations"},
		"verb":     {"handle", "organize", "review", "document", "deploy", "monitor", "test", "budget for"},
		"place":    {"the North Sea", "rural Norway", "the Atacama desert", "Lisbon", "Osaka", "the Great Lakes", "Antarctica", "a small Dutch town"},
		"year":     {"2026", "this decade", "a post-cloud world"},
		"product":  {"Ledgerly", "Tidepool", "Kiteboard", "Quarry", "Moth", "Pebble", "Loom", "Fernwood", "Brightline", "Cinder"},
	}
	companyA = []string{"Northwind", "Harbor", "Maple", "Lattice", "Kestrel", "Fernwood", "Cinder", "Tidepool", "Brightline", "Quarry"}
	companyB = []string{"Labs", "Systems", "Cloud", "Robotics", "Health", "Data", "Energy", "Finance"}
	roles    = []string{"backend engineers", "a founding engineer", "an SRE", "a compiler engineer", "product designers", "a data engineer", "an embedded engineer"}
	places   = []string{"Remote", "Berlin", "Toronto", "Remote US", "Lisbon", "Austin", "Singapore", "Remote EU"}
)
