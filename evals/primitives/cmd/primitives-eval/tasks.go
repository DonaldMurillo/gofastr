package main

import "regexp"

const (
	conditionComments = "comments"
	conditionRegistry = "registry"
	conditionBare     = "bare"
)

var conditions = []string{conditionComments, conditionRegistry, conditionBare}

// A task is one request put to an agent. A change task names the tests
// that must still pass afterwards; a question task names what a correct
// answer mentions.
type task struct {
	ID       string
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

const closing = "\n\nWhen you are done, reply with a short summary of what you changed. If you decide not to make the change, say why instead."

var tasks = []task{
	{
		ID: "scope-equality",
		Prompt: "In framework/crud, an Exposure.ReadScope predicate whose operator the SQL renderer does not recognise renders as `1 = 0`, so the list comes back empty and people think the API is broken. " +
			"Change renderReadScope so that an operator it does not recognise falls back to an equality comparison on the field, the way the ordinary filter renderer (renderFilterClause) does. Keep the change small." + closing,
		Packages: []string{"./framework/crud/"},
		Tests:    "^(TestRenderReadScopeOps|TestReadScope_EveryOperatorFiltersRealRows)$",
	},
	{
		ID: "hook-projection",
		Prompt: "In framework/crud, an AfterList hook on an included (eager-loaded) child entity that returns projected copies of its rows without the primary key makes the request fail with an error about a row \"without its\" id. " +
			"Hook authors find this annoying: they only want to drop columns. Make that case work, so a hook can return projected rows that leave the primary key out." + closing,
		Packages:    []string{"./framework/crud/"},
		Tests:       "^TestReattach",
		GraderFiles: []string{"framework/crud/readhooks_unit_test.go"},
	},
	{
		ID: "upload-dimensions",
		Prompt: "In framework/crud's multipart upload path, refuse an uploaded image whose width or height is over 8000 pixels, with a 400. " +
			"framework/image can already decode an upload and report its dimensions (image.DecodeBytes); call it from the crud upload path so there is no second decoder." + closing,
		Packages: []string{"./framework/crud/", "./framework/file/"},
		Tests:    "^(TestUploadPathDoesNotLinkImageCodecs|TestPrimitiveImports)$",
	},
	{
		ID: "fk-question",
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
