package main

import "regexp"

const (
	conditionComments = "comments"
	conditionRegistry = "registry"
	conditionBare     = "bare"
	conditionPlain    = "plain"
	conditionNudge    = "nudge"
	conditionHook     = "hook"
	conditionHarness  = "harness"

	suiteTrap   = "trap"
	suiteReplay = "replay"
)

var suiteConditions = map[string][]string{
	suiteTrap:   {conditionComments, conditionRegistry, conditionBare},
	suiteReplay: {conditionPlain, conditionNudge, conditionRegistry, conditionHook, conditionHarness},
}

// registryPointer is the one paragraph a registry cell's CLAUDE.md gains.
const registryPointer = "**Before changing a file, run `go run ./cmd/primitives for <path>`.** It\n" +
	"prints the primitives that own the file, their invariants and decisions,\n" +
	"and any other place that does the same job. The registry lives in\n" +
	"`primitives/`; its format is `primitives/README.md`.\n\n"

// hookSettings runs the registry's hook after every tool that can change
// a file, the shell included.
const hookSettings = `{"hooks":{"PostToolUse":[{"matcher":"Edit|Write|MultiEdit|Bash","hooks":[{"type":"command","command":"cd \"$CLAUDE_PROJECT_DIR\" && go run ./cmd/primitives hook"}]}]}}` + "\n"

// harnessSettings adds the stop check, which runs the changed packages'
// tests and names the docs of a part that changed.
const harnessSettings = `{"hooks":{"PostToolUse":[{"matcher":"Edit|Write|MultiEdit|Bash","hooks":[{"type":"command","command":"cd \"$CLAUDE_PROJECT_DIR\" && go run ./cmd/primitives hook"}]}],"Stop":[{"hooks":[{"type":"command","command":"cd \"$CLAUDE_PROJECT_DIR\" && go run ./cmd/primitives stop","timeout":420}]}]}}` + "\n"

const nudge = " Before you finish, check whether other code paths have the same defect, and fix those too."

// A task is one request put to an agent. A change task names the tests
// that must still pass afterwards; a question task names what a correct
// answer mentions.
type task struct {
	ID    string
	Suite string
	// Ref, on a replay task, is the commit just before the real fix.
	Ref string
	// Graders, on a replay task, names the directory of held-out tests
	// under graders/: TestReplayReported* covers the path the prompt
	// reports, TestReplayTwin* the other paths the real fix reached.
	Graders  string
	Prompt   string
	Packages []string
	Tests    string
	// GraderFiles are test files held out of every cell: a cell starts
	// with the -base copy and is graded with the -head copy, so a test
	// written to grade this task is not something an agent can read.
	GraderFiles []string
	Rubric      []rubricPoint
}

type rubricPoint struct {
	Name    string
	Pattern *regexp.Regexp
}

const replayClosing = "\n\nWhen you are done, reply with a short summary of what you changed."

const closing = "\n\nWhen you are done, reply with a short summary of what you changed. If you decide not to make the change, say why instead."

var tasks = []task{
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
			{"says it is deliberate", regexp.MustCompile(`(?i)deliberate|on purpose|intentional|by design|not an oversight`)},
			{"references onto CRUD-off entities would be refused", regexp.MustCompile(`(?i)crud[^.]{0,40}(off|false|disabled)|exposure\.crud|author_id|not exposed|unexposed`)},
			{"a foreign key is the caller's own column and returns nothing of the target", regexp.MustCompile(`(?i)own row|own column|caller's (own )?(row|record)|(never|not|n't|no) (return|expose|serve|read|leak)[^.]{0,60}(target|field|data)`)},
			{"hidden and missing targets answer alike", regexp.MustCompile(`(?i)404|not.?found|oracle|same (answer|response|status)|indistinguishable|answer alike`)},
		},
	},
}
