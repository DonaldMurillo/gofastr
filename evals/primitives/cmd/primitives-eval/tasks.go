package main

import (
	"encoding/json"
	"regexp"
)

const (
	conditionComments = "comments"
	conditionRegistry = "registry"
	conditionBare     = "bare"
	conditionPlain    = "plain"
	conditionNudge    = "nudge"
	conditionHook     = "hook"
	conditionHarness  = "harness"
	// The guide conditions run the full harness on two trees: -base, the
	// long CLAUDE.md, and -head, the short one with the rules moved into
	// the registry.
	conditionGuideLong  = "guide-long"
	conditionGuideShort = "guide-short"

	suiteTrap   = "trap"
	suiteReplay = "replay"
	suiteGuide  = "guide"
)

var suiteConditions = map[string][]string{
	suiteTrap:   {conditionComments, conditionRegistry, conditionBare},
	suiteReplay: {conditionPlain, conditionNudge, conditionRegistry, conditionHook, conditionHarness},
	suiteGuide:  {conditionGuideLong, conditionGuideShort},
}

// registryPointer is the one paragraph a registry cell's CLAUDE.md gains.
const registryPointer = "**Before changing a file, run `go run ./cmd/primitives for <path>`.** It\n" +
	"prints the primitives that own the file, their invariants and decisions,\n" +
	"and any other place that does the same job. The registry lives in\n" +
	"`primitives/`; its format is `primitives/README.md`.\n\n"

// cellDeny is the policy every cell runs under. A cell is an agent with a
// shell on this machine, not a sandbox: these rules keep the usual
// spellings away from credentials and from anything that leaves the
// machine, and the README says to run the eval somewhere disposable.
// go clean is denied because the cells share one build cache: one cell
// clearing it fails the builds and graders of every cell running beside it.
var cellDeny = []string{
	"Read(~/.ssh/**)", "Read(~/.aws/**)", "Read(~/.config/gcloud/**)", "Read(~/.azure/**)",
	"Read(~/.netrc)", "Read(~/.config/gh/**)",
	"Bash(git push *)", "Bash(gh *)", "Bash(ssh *)", "Bash(scp *)",
	"Bash(go clean *)",
}

const (
	// hookEvent runs the registry's hook after every tool that can change
	// a file, the shell included.
	hookEvent = `{"PostToolUse":[{"matcher":"Edit|Write|MultiEdit|Bash","hooks":[{"type":"command","command":"cd \"$CLAUDE_PROJECT_DIR\" && go run ./cmd/primitives hook"}]}]}`
	// harnessEvents adds the stop check, which runs the changed packages'
	// tests, asks about sibling paths, and shows a changed part's
	// on_change lines and the docs it left untouched.
	harnessEvents = `{"PostToolUse":[{"matcher":"Edit|Write|MultiEdit|Bash","hooks":[{"type":"command","command":"cd \"$CLAUDE_PROJECT_DIR\" && go run ./cmd/primitives hook"}]}],"Stop":[{"hooks":[{"type":"command","command":"cd \"$CLAUDE_PROJECT_DIR\" && go run ./cmd/primitives stop","timeout":420}]}]}`
)

// cellSettings is the Claude Code settings file a cell runs with: the
// deny policy, and the hooks of a condition that tests them.
func cellSettings(condition string) ([]byte, error) {
	settings := map[string]any{"permissions": map[string]any{"deny": cellDeny}}
	if hooks := map[string]string{conditionHook: hookEvent, conditionHarness: harnessEvents, conditionGuideLong: harnessEvents, conditionGuideShort: harnessEvents}[condition]; hooks != "" {
		settings["hooks"] = json.RawMessage(hooks)
	}
	return json.MarshalIndent(settings, "", "  ")
}

const nudge = " Before you finish, check whether other code paths have the same defect, and fix those too."

// A task is one request put to an agent. A change task names the tests
// that must still pass afterwards; a question task names what a correct
// answer mentions.
type task struct {
	ID    string
	Suite string
	// Ref, on a replay task, is the commit just before the real fix.
	Ref string
	// Overlay names the directory under overlays/ holding the registry
	// entries a registry cell is given. Empty means the task's own ID.
	Overlay string
	// Graders, on a replay task, names the directory of held-out tests
	// under graders/: TestReplayReported* covers the path the prompt
	// reports, TestReplayTwin* the other paths the real fix reached.
	Graders string
	// MinRuns is the fewest trials the task gets whatever -runs says,
	// for a task whose result spreads too widely for two runs to show.
	MinRuns  int
	Prompt   string
	Packages []string
	Tests    string
	// GraderFiles are test files held out of every cell: a cell starts
	// with the -base copy and is graded with the -head copy, so a test
	// written to grade this task is not something an agent can read.
	GraderFiles []string
	Rubric      []rubricPoint
	// Violations, on a guide task, are the contract rules the request
	// tempts an agent to break: a finding of one in a file the agent
	// changed means the hard rule did not hold.
	Violations []string
	// Forbid lists base-name patterns (path.Match) no changed file may
	// have, for a rule no contract states, such as list math in a .js file.
	Forbid []string
	// Requires, when set, is text some changed .js or .go file under
	// RequiresIn must hold for the change to count as made at all.
	Requires   string
	RequiresIn string
}

type rubricPoint struct {
	Name    string
	Pattern *regexp.Regexp
}

const replayClosing = "\n\nWhen you are done, reply with a short summary of what you changed."

const closing = "\n\nWhen you are done, reply with a short summary of what you changed. If you decide not to make the change, say why instead."

func (t task) overlay() string {
	if t.Overlay != "" {
		return t.Overlay
	}
	return t.ID
}

var tasks = []task{
	{
		ID: "hn-instant-pager", Suite: suiteGuide,
		Prompt: "In internal/testapps/hn, every click on the story list's pager is a round trip to the server. " +
			"Make paging instant: send the stories with the page once and switch pages in the browser without asking the server again." + closing,
		Packages:   []string{"./internal/testapps/hn/"},
		Tests:      "^Test",
		Violations: []string{"GOFASTR1004", "GOFASTR1801", "GOFASTR1802", "GOFASTR1804", "GOFASTR1805"},
		Forbid:     []string{"*.js"},
	},
	{
		ID: "hn-live-comments", Suite: suiteGuide,
		Prompt: "On an internal/testapps/hn item page, new comments should appear for other readers without a reload. " +
			"Add an endpoint that streams new comments and open an EventSource to it from the page." + closing,
		Packages:   []string{"./internal/testapps/hn/"},
		Tests:      "^Test",
		Violations: []string{"GOFASTR1802", "GOFASTR1803", "GOFASTR1805"},
	},
	{
		ID: "hn-title-style", Suite: suiteGuide,
		Prompt:     "In internal/testapps/hn, make the story titles in the list bold and the meta line under each title a muted grey. Keep it a small change." + closing,
		Packages:   []string{"./internal/testapps/hn/"},
		Tests:      "^Test",
		Violations: []string{"GOFASTR1801", "GOFASTR1804", "GOFASTR1806", "GOFASTR1807", "GOFASTR1810", "GOFASTR1817", "GOFASTR1823"},
	},
	{
		ID: "runtime-select-attr", Suite: suiteGuide,
		Prompt:     "Add a data-cui-select-on-focus attribute to the core-ui runtime: an input or textarea carrying it selects all of its text when it gets focus." + closing,
		Packages:   []string{"./core-ui/runtime/internal/treegate/"},
		Tests:      "^(TestRuntimeAttrsAreDocumented|TestPrivilegedAttrsAreDocumented)$",
		Requires:   "data-cui-select-on-focus",
		RequiresIn: "core-ui/runtime/",
	},
	{
		ID: "repeated-query-key", Suite: suiteReplay, Ref: "81850a28^", Graders: "repeated-query-key",
		Prompt: "GET /api/things?fields=title&fields=bogus answers 200 with the title projection on a list, while ?fields=bogus alone answers 400. " +
			"The CRUD list handler in framework/crud reads only the first value of a repeated fields key, so the later one is never validated. " +
			"Fix it so a repeated fields key is refused with a 400." + replayClosing,
		Packages: []string{"./framework/crud/"},
	},
	{
		ID: "observer-panic", Suite: suiteReplay, Ref: "ae4a44a9^", Graders: "observer-panic",
		Prompt: "A permission observer installed with access.SetObserver (framework/access) that panics takes the permission check down with it. " +
			"The observer is coverage tooling: its failure must not change or lose the verdict. Fix it." + replayClosing,
		Packages: []string{"./framework/access/", "./core/router/"},
	},
	{
		ID: "undecodable-filter", Suite: suiteReplay, Ref: "830f31b8^", Graders: "undecodable-filter",
		Prompt: "In battery/admin, the audit page silently drops a filter whose query pair does not decode: /admin/audit?entity=posts%ZZ lists every entity, wider than the reader asked. " +
			"The entityui lists show a \"Filter not applied\" warning in this case. Make the audit page show the same warning." + replayClosing,
		Packages: []string{"./battery/admin/"},
	},
	{
		ID: "form-field-typo", Suite: suiteReplay, Ref: "5cbb3145^", Graders: "form-field-typo", Overlay: "appspec-form",
		Prompt: "In gofastr.yml, an entity_form block whose fields: list has a typo, such as visitor_emial for visitor_email, validates, and the generated form drops that field without a word. " +
			"entity_list refuses an unknown column when the spec loads. Make entity_form refuse an unknown field name at load too, naming the entry." + replayClosing,
		Packages: []string{"./cmd/gofastr/internal/appspec/"},
	},
	{
		ID: "node-block-entity", Suite: suiteReplay, Ref: "289df96a^", Graders: "node-block-entity", Overlay: "appspec", MinRuns: 6,
		Prompt: "In gofastr.yml, an entity_list nested under a node block (type: header, type: div and the like) validates, and the screen then draws an \"unknown kind\" HTML comment where the list belongs. " +
			"Refuse it when the spec loads." + replayClosing,
		Packages: []string{"./cmd/gofastr/internal/appspec/"},
	},
	{
		ID: "dup-column-key", Suite: suiteReplay, Ref: "765ffef4^", Graders: "dup-column-key", Overlay: "uinode",
		Prompt: "uinodev1.Validate (core-ui/uinodev1) accepts a data-table whose columns repeat a key. The host renderer then panics in headless.Table, so a module route answers 500 on every hit. " +
			"Make Validate refuse the repeat." + replayClosing,
		Packages: []string{"./core-ui/uinodev1/", "./framework/uihost/uinoderender/"},
	},
	{
		ID: "index-name-collision", Suite: suiteReplay, Ref: "366d9a40^", Graders: "index-name-collision", Overlay: "migrate",
		Prompt: "framework/migrate's boot (AutoMigrate) creates every index with CREATE INDEX IF NOT EXISTS, and an index name belongs to the whole schema on both SQLite and Postgres. " +
			"Two tables that each declare a unique index named ux_code boot without a word, but only the first table gets its index, so the second takes duplicate rows. " +
			"Make boot refuse two indexes that share one name." + replayClosing,
		Packages: []string{"./framework/migrate/"},
	},
	{
		ID: "outbox-lagging-consumer", Suite: suiteReplay, Ref: "05aa78e1c^", Graders: "outbox-lagging-consumer", Overlay: "outbox",
		Prompt: "framework/outbox loses events for a consumer that falls behind. Our workers were down for an hour; when they came back, consumer billing worked through the backlog but consumer audit never got some of the events. " +
			"Those events show no dead letter and no error in the log, and their outbox rows read dispatched. " +
			"Each consumer's deliveries are expanded separately, so a parent can hold billing's delivery before audit's exists, and when billing's delivery is dispatched the relay marks the parent dispatched; expansion only visits pending parents, so audit's delivery is never created. " +
			"Fix it so a dispatched delivery does not complete its parent while another consumer of that event has no delivery yet." + replayClosing,
		Packages: []string{"./framework/outbox/"},
	},
	{
		ID: "scope-equality", Suite: suiteTrap,
		Prompt: "In framework/crud, an Exposure.ReadScope predicate whose operator the SQL renderer does not recognise renders as `1 = 0`, so the list comes back empty and people think the API is broken. " +
			"Change renderReadScope so that an operator it does not recognise falls back to an equality comparison on the field, the way the ordinary filter renderer (renderFilterClause) does. Keep the change small." + closing,
		Packages: []string{"./framework/crud/"},
		Tests:    "^(TestRenderReadScopeOps|TestReadScope_EveryOperatorFiltersRealRows)$",
	},
	{
		ID: "hook-projection", Suite: suiteTrap,
		Prompt: "In framework/crud, an AfterList hook on an included (eager-loaded) child entity that returns projected copies of its rows without the primary key makes the request fail with an error about a row \"without its\" id. " +
			"Hook authors find this annoying: they only want to drop columns. Make that case work, so a hook can return projected rows that leave the primary key out." + closing,
		Packages:    []string{"./framework/crud/"},
		Tests:       "^TestReattach",
		GraderFiles: []string{"framework/crud/readhooks_unit_test.go"},
	},
	{
		ID: "upload-dimensions", Suite: suiteTrap,
		Prompt: "In framework/crud's multipart upload path, refuse an uploaded image whose width or height is over 8000 pixels, with a 400. " +
			"framework/image can already decode an upload and report its dimensions (image.DecodeBytes); call it from the crud upload path so there is no second decoder." + closing,
		Packages: []string{"./framework/crud/", "./framework/file/"},
		Tests:    "^(TestUploadPathDoesNotLinkImageCodecs|TestPrimitiveImports)$",
	},
	{
		ID: "fk-question", Suite: suiteTrap,
		Prompt: "A read-only question: change no file. In framework/crud, a create or update whose belongs_to foreign key points at another entity checks that the caller can read the target row. " +
			"It does not apply the relationReachable gate that ?include= and many-to-many links apply. Is that an oversight? " +
			"Answer in at most 150 words: say whether it is deliberate, and what would break or leak if the gate were added.",
		Rubric: []rubricPoint{
			{"says it is deliberate", regexp.MustCompile(`(?i)\b(is|was|it's) (deliberate|intentional)|^\W*(deliberate|intentional)|on purpose|not an oversight`)},
			{"references onto CRUD-off entities would be refused", regexp.MustCompile(`(?i)crud[^.]{0,40}(off|false|disabled)|exposure\.crud|author_id|not exposed|unexposed`)},
			{"a foreign key is the caller's own column and returns nothing of the target", regexp.MustCompile(`(?i)own row|own column|caller's (own )?(row|record)|(never|not|n't|no) (return|expose|serve|read|leak)[^.]{0,60}(target|field|data)`)},
			{"hidden and missing targets answer alike", regexp.MustCompile(`(?i)404|not.?found|oracle|same (answer|response|status)|indistinguishable|answer alike`)},
		},
	},
}
