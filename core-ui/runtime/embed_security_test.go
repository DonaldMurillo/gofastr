package runtime

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

type embedSourceControl struct {
	name     string
	file     string
	literal  string
	minCount int
	mutation string
}

var existingEmbedSourceControls = []embedSourceControl{
	{name: "frame accepts messages only from its parent", file: "frag/boot-embed.js", literal: "if (e.source !== window.parent) return;", minCount: 1, mutation: "if (false) return;"},
	{name: "frame accepts only the first token", file: "frag/boot-embed.js", literal: "if (started) return;", minCount: 1, mutation: "if (false) return;"},
	{name: "frame posts state only to the attested parent origin", file: "frag/boot-embed.js", literal: "window.parent.postMessage(msg, target)", minCount: 1, mutation: "window.parent.postMessage(msg, '*')"},
	{name: "grant wrapper rejects cross-origin requests", file: "frag/boot-embed.js", literal: "if (url.origin !== location.origin) return _fetch(input, init);", minCount: 1, mutation: "if (false) return _fetch(input, init);"},
	{name: "grant wrapper omits cookies", file: "frag/boot-embed.js", literal: "opts.credentials = 'omit';", minCount: 1, mutation: "opts.credentials = 'same-origin';"},
	{name: "grant wrapper refuses redirects", file: "frag/boot-embed.js", literal: "opts.redirect = 'error';", minCount: 1, mutation: "opts.redirect = 'follow';"},
	{name: "loader accepts messages only from its frame", file: "embed-loader.js", literal: "if (e.source !== frame.contentWindow) return;", minCount: 1, mutation: "if (false) return;"},
	{name: "loader accepts messages only from the app origin", file: "embed-loader.js", literal: "if (e.origin !== origin) return;", minCount: 1, mutation: "if (false) return;"},
	{name: "loader posts nonce only to the app origin", file: "embed-loader.js", literal: "frame.contentWindow.postMessage({ proto: PROTO, type: 'token', token: token }, origin);", minCount: 1, mutation: "frame.contentWindow.postMessage({ proto: PROTO, type: 'token', token: token }, '*');"},
	{name: "loader clamps reported height", file: "embed-loader.js", literal: "Math.min(Math.ceil(h), 20000)", minCount: 1, mutation: "Math.ceil(h)"},
	{name: "loader suppresses referrer", file: "embed-loader.js", literal: "frame.setAttribute('referrerpolicy', 'no-referrer');", minCount: 1, mutation: ""},
	{name: "loader prevents duplicate target mounts", file: "embed-loader.js", literal: "if (target.__gofastrEmbed) return target.__gofastrEmbed;", minCount: 1, mutation: ""},
}

var containmentEmbedSourceControls = []embedSourceControl{
	{name: "iframe has the minimal sandbox", file: "embed-loader.js", literal: "frame.setAttribute('sandbox', 'allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox');", minCount: 1, mutation: ""},
	{name: "frame captures link navigation", file: "frag/boot-embed.js", literal: "window.addEventListener('click', blockLinkNavigation, true);", minCount: 1, mutation: ""},
	{name: "a new-tab link is opened, not refused", file: "frag/boot-embed.js", literal: "window.open(href, '_blank', 'noopener')", minCount: 1, mutation: ""},
	{name: "frame captures form navigation", file: "frag/boot-embed.js", literal: "window.addEventListener('submit', blockFormNavigation, true);", minCount: 1, mutation: ""},
	{name: "link and form navigation are cancelled", file: "frag/boot-embed.js", literal: "e.preventDefault();", minCount: 2, mutation: ""},
	{name: "blocked link is reported", file: "frag/boot-embed.js", literal: "showBlockedNavigation('link'", minCount: 1, mutation: ""},
	{name: "blocked form is reported", file: "frag/boot-embed.js", literal: "showBlockedNavigation('form'", minCount: 1, mutation: ""},
	{name: "blocked navigation is visible", file: "frag/boot-embed.js", literal: "notice.setAttribute('role', 'alert');", minCount: 1, mutation: ""},
}

var livenessEmbedSourceControls = []embedSourceControl{
	{name: "requests have a fixed deadline", file: "frag/boot-embed.js", literal: "const REQUEST_TIMEOUT_MS = 15000;", minCount: 1, mutation: "const REQUEST_TIMEOUT_MS = 0;"},
	{name: "deadline aborts the underlying request", file: "frag/boot-embed.js", literal: "new AbortController()", minCount: 1, mutation: "null"},
	{name: "exchange refresh and content receive abort signals", file: "frag/boot-embed.js", literal: "signal: signal", minCount: 3, mutation: "signal: undefined"},
	{name: "exchange refresh and content are deadline-bounded", file: "frag/boot-embed.js", literal: "await withDeadline(", minCount: 3, mutation: "await ("},
	{name: "failure state has visible text", file: "frag/boot-embed.js", literal: "root.textContent = 'This panel could not load. Error: ' + code + '.';", minCount: 1, mutation: "root.textContent = '';"},
}

func readEmbedControlSources(t *testing.T) map[string]string {
	t.Helper()
	files := []string{"embed-loader.js", "frag/boot-embed.js"}
	sources := make(map[string]string, len(files))
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		sources[file] = string(body)
	}
	return sources
}

func missingEmbedSourceControls(sources map[string]string, controls []embedSourceControl) []string {
	var missing []string
	for _, control := range controls {
		if strings.Count(sources[control.file], control.literal) < control.minCount {
			missing = append(missing, control.name)
		}
	}
	return missing
}

func applyRequestedEmbedMutation(sources map[string]string, controls []embedSourceControl) {
	requested := os.Getenv("GOFASTR_EMBED_SOURCE_MUTATION")
	if requested == "" {
		return
	}
	for _, control := range controls {
		if requested != "all" && requested != control.name {
			continue
		}
		sources[control.file] = strings.ReplaceAll(sources[control.file], control.literal, control.mutation)
	}
}

func requireEmbedSourceControls(t *testing.T, controls []embedSourceControl) {
	t.Helper()
	sources := readEmbedControlSources(t)
	applyRequestedEmbedMutation(sources, controls)
	if missing := missingEmbedSourceControls(sources, controls); len(missing) > 0 {
		t.Fatalf("embed security source gate rejected %d missing control(s):\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}

func TestEmbedExistingSecurityControls(t *testing.T) {
	requireEmbedSourceControls(t, existingEmbedSourceControls)
}

func TestEmbedContainmentControls(t *testing.T) {
	requireEmbedSourceControls(t, containmentEmbedSourceControls)
}

func TestEmbedRequestLivenessControls(t *testing.T) {
	requireEmbedSourceControls(t, livenessEmbedSourceControls)
}

func TestEmbedSourceGateRejectsEveryMutation(t *testing.T) {
	controls := append(append([]embedSourceControl{}, existingEmbedSourceControls...), containmentEmbedSourceControls...)
	controls = append(controls, livenessEmbedSourceControls...)
	sources := readEmbedControlSources(t)
	for _, control := range controls {
		control := control
		t.Run(control.name, func(t *testing.T) {
			mutated := map[string]string{
				"embed-loader.js":    sources["embed-loader.js"],
				"frag/boot-embed.js": sources["frag/boot-embed.js"],
			}
			mutated[control.file] = strings.ReplaceAll(mutated[control.file], control.literal, control.mutation)
			if missing := missingEmbedSourceControls(mutated, []embedSourceControl{control}); len(missing) != 1 {
				t.Fatalf("mutation %q did not trip its source assertion; missing=%v", control.name, missing)
			}
		})
	}
}

// sandboxTokenRe extracts the sandbox attribute value the loader actually sets.
var sandboxTokenRe = regexp.MustCompile(`setAttribute\('sandbox',\s*'([^']*)'\)`)

// The presence gate above pins the exact sandbox string, but it reads the whole
// file, so it can only catch a token being REMOVED. The risk here is the
// opposite: a token being ADDED. Top navigation is the one that matters — with
// it, an embeddable screen containing <a target="_top"> replaces the CUSTOMER's
// page, which is the failure the sandbox was introduced to stop.
func TestSandboxNeverGrantsTopNavigation(t *testing.T) {
	sources := readEmbedControlSources(t)
	m := sandboxTokenRe.FindStringSubmatch(sources["embed-loader.js"])
	if m == nil {
		t.Fatal("embed-loader.js sets no sandbox attribute — the frame can navigate the customer's page")
	}
	allowed := map[string]bool{
		"allow-scripts":                  true,
		"allow-same-origin":              true,
		"allow-popups":                   true,
		"allow-popups-to-escape-sandbox": true,
	}
	for _, tok := range strings.Fields(m[1]) {
		if !allowed[tok] {
			t.Fatalf("sandbox grants %q. Adding a token widens what the frame may do to the "+
				"page hosting it; anything top-navigation-shaped hands it the customer's page. "+
				"If the token is genuinely needed, add it to this list deliberately.", tok)
		}
	}
}
