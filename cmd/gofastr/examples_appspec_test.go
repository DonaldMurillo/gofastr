package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

// buildGateScratchPkg prefixes the throwaway package each app spec is
// generated into, under internal/scratchdir's Root (see that package for
// why it lives in the repo module and why there). The prefix keeps it apart
// from the scratch packages other test binaries generate beside it.
const buildGateScratchPkg = "build_"

// TestExampleAppSpecsBoot is the gate that matters: every shipped app spec
// must emit Go that builds, vets, starts and serves.
//
// Why this exists, generalized, rather than per-example:
//
// examples/meridian/appspec_gate_test.go already made exactly this argument
// for one app spec ("Without this gate, gofastr.yml can rot silently, which is
// exactly how #131 went unnoticed"), and examples/ecommerce reaches the same
// guarantee a different way: its committed app/ is compiled by `go build
// ./...` and a byte-parity test pins the generator to it.
//
// Those two were the ONLY app specs whose emitted Go was ever compiled. The
// other five, blog, lms, portfolio, project-manager, real-estate, were
// covered solely by TestExampleAppSpecsLoad (internal/appspec), which checks
// that the YAML parses. All five emitted code that did not compile, and
// neither of the two gated examples could catch it: ecommerce declares no
// home screen and no `access:` role policy, so it is the one example that
// never reaches either broken generator path.
//
// The lesson is that a gate aimed at one fixture proves one fixture. This test
// is aimed at all seven, so a generator path is covered the moment any
// app spec uses it.
//
// Compiling was not enough either: the blog app spec rotted for a whole
// release while it still built. Its seed seeded `post_id: 1` into a UUID
// relation column, an app that builds cleanly and dies at boot ("seed
// hooks: seed comments: validation failed"). So each example is generated,
// compiled (the main package, every package, go vet, the repo's vettool),
// STARTED and exercised. A compile-only test ran beside this one and built
// every example a second time, and appgen's vet gate a third; nothing they
// checked is missing here.
//
// Failure modes asserted: broken emitted Go, emitted Go the repo's own
// analyzers refuse, non-zero exit before serving (the seed-death class),
// failure to bind/serve within the deadline, and error output on the way
// down. The child runs in its own process group and is killed by tree even
// when the test fails mid-flight, so no app leaks past the run (CI-safe on
// Linux and macOS via configureTestProcessGroup).
//
// Each example is a variant build (clitest.VariantBuild): a pull request
// boots the examples whose app spec it edits, and appgen's TestUnionApp
// checks a generator change. The example's tree is shared (exampleTree):
// the lms and portfolio security tests boot the same build.
//
//gofastr:test-reads examples/*/gofastr.yml
func TestExampleAppSpecsBoot(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, compiles, and boots every example app spec; skipped under -short")
	}
	for _, path := range clitest.ExampleAppSpecs(t) {
		path := path
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			clitest.VariantBuild(t, "examples/"+name+"/gofastr.yml")
			// Each example renders into its own scratch package
			// (_scratch/appspecs/build_<name>) and boots on a fresh port,
			// so the seven can overlap.
			t.Parallel()
			bin, appDir := exampleTree(t, path, name)
			t.Run("vettool", func(t *testing.T) { vetWithRepoVettool(t, appDir) })
			baseURL, output, _ := bootGeneratedApp(t, name, bin, appDir)
			exerciseGeneratedApp(t, name, baseURL, output)
		})
	}
}

// exerciseGeneratedApp is the rung above "it booted". The boot probe accepts
// any non-5xx answer to GET /, which proves the process is alive and routing,
// and nothing else. Every generated surface past the homepage (the REST list
// routes, the MCP tools, the authorization posture shared by both) was
// unexercised by any gate, so a generated app could serve a homepage and be
// wrong about everything a user would actually call.
//
// The check is derived from the app rather than hardcoded per app spec: ask
// /mcp which tools exist, and for every "<table>_list" tool compare the MCP
// answer's posture against the REST list route's. The invariant is parity:
// tool calls re-enter the same router and Exposure as the REST handlers, so
// the two must agree about whether the caller may read. A tools/call that
// succeeds where REST answers 401/403 is an unguarded second authorization
// path, which is the failure this exists to catch; the reverse (REST open,
// tool refused) is a silently broken agent surface.
func exerciseGeneratedApp(t *testing.T, name, baseURL string, output *clitest.SyncBuffer) {
	t.Helper()

	status, body := postMCP(t, baseURL, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if status == http.StatusNotFound {
		t.Logf("%s: no /mcp endpoint (app spec does not enable MCP) — skipping the parity exercise", name)
		return
	}
	if status != http.StatusOK {
		t.Fatalf("%s: POST /mcp tools/list = %d, want 200:\n%s\n%s", name, status, body, output.String())
	}

	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &listResp); err != nil {
		t.Fatalf("%s: tools/list is not JSON: %v\n%s", name, err, body)
	}
	if len(listResp.Result.Tools) == 0 {
		t.Fatalf("%s: /mcp is mounted but exposes no tools — every generated entity should carry its CRUD tools:\n%s", name, body)
	}

	checked := 0
	// servedRows / valuesCompared track the value-level half of the screen
	// gate, which returns early when a table is empty. Without them, "every
	// table looked empty" is indistinguishable from "every screen was checked".
	servedRows, valuesCompared := 0, 0
	for _, tool := range listResp.Result.Tools {
		table, ok := strings.CutSuffix(tool.Name, "_list")
		if !ok {
			continue
		}
		restStatus, restPath := restListStatus(t, baseURL, table)
		if restStatus == 0 {
			// No REST list route for this tool's table (introspection tools,
			// or an entity exposed to MCP only). Nothing to compare against.
			continue
		}
		callStatus, callBody := postMCP(t, baseURL,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"`+tool.Name+`","arguments":{}}}`)
		if callStatus != http.StatusOK {
			t.Fatalf("%s: POST /mcp tools/call %s = HTTP %d, want 200 (JSON-RPC reports its own errors in the body): %s",
				name, tool.Name, callStatus, callBody)
		}
		// Classify on the authorization signal, not on "an error happened".
		// Any JSON-RPC failure, such as a missing required argument or a
		// 500, used to read as "refused", so a tool that was merely broken
		// counted as correctly gated and the assertion passed vacuously.
		mcpErr, mcpFailed := mcpErrorMessage(callBody)
		mcpRefused := strings.Contains(mcpErr, "status 401") ||
			strings.Contains(mcpErr, "status 403") ||
			strings.Contains(mcpErr, "authentication required") ||
			strings.Contains(mcpErr, "missing permission") ||
			strings.Contains(mcpErr, "access denied")
		restRefused := restStatus == http.StatusUnauthorized || restStatus == http.StatusForbidden

		if !restRefused && mcpFailed {
			t.Fatalf("%s: REST %s is open (%d) but the MCP tool %s failed: a tool that errors for a NON-auth reason compares equal to a refusal and makes this parity check pass vacuously.\n%s",
				name, restPath, restStatus, tool.Name, callBody)
		}
		if restRefused != mcpRefused {
			t.Fatalf("%s: authorization parity broken for %q — REST %s = %d (refused=%v) but MCP tools/call refused=%v.\n"+
				"MCP tool calls must inherit the entity's Exposure exactly; a divergence here is a second, unguarded auth path.\nMCP response: %s",
				name, table, restPath, restStatus, restRefused, mcpRefused, callBody)
		}
		// The third surface. REST and MCP share the route middleware; a
		// server-rendered screen does not enter it at all, so an entity could
		// answer 403 on /api/<table> and 200, with every row in the HTML, on
		// the /<table> screen the same app spec generated. That was live on
		// the flagship blog app spec: GET /api/users refused while GET /users
		// served three user emails.
		if restRefused {
			assertScreenServesNoRows(t, name, baseURL, table)
		} else {
			served, compared := assertScreenServesRows(t, name, baseURL, table, restPath)
			if served {
				servedRows++
			}
			if compared {
				valuesCompared++
			}
		}
		checked++
	}
	// Fatal, not Logf. Every assertion above lives inside the loop, so a
	// `checked` of zero means this whole gate, REST/MCP parity AND both
	// screen directions, silently exercised nothing while the test reported
	// a pass. Tool-name drift or an API-prefix change would do it: the
	// `_list` suffix stops matching, or restListStatus stops finding a route,
	// and every iteration `continue`s. The app spec reached here with tools
	// mounted, so at least one of them must pair with a REST route.
	if checked == 0 {
		t.Fatalf("%s: /mcp exposes %d tools but not one paired with a REST list route — "+
			"the parity and screen assertions ran zero times and this gate proved nothing",
			name, len(listResp.Result.Tools))
	}
	// `checked` counts entities that reached the screen assertions, but the
	// value-level half only runs when the REST route actually served rows AND
	// the screen answered 200. If rows existed everywhere and nothing was ever
	// compared, that half verified nothing while the count still looked
	// healthy: a seed that stopped running, a list envelope this gate can no
	// longer read, or every screen quietly redirecting all present that way.
	if servedRows > 0 && valuesCompared == 0 {
		t.Fatalf("%s: %d entities served rows over REST but not one screen was checked against an actual value — "+
			"the too-tight-gate direction verified nothing", name, servedRows)
	}
	t.Logf("%s: REST/MCP authorization parity verified for %d entities (%d value-compared)", name, checked, valuesCompared)
}

// restListStatus finds the generated REST list route for a table and returns
// its status. Generated apps mount entities under an API prefix that the
// app spec chooses (/api/<table> by default, /<table> when the app declares no
// prefix), so both shapes are probed and the first that is not a 404 wins. A
// zero status means the table has no REST list route at all.
func restListStatus(t *testing.T, baseURL, table string) (int, string) {
	t.Helper()
	for _, path := range []string{"/api/" + table, "/" + table} {
		resp, err := bootProbeClient.Get(baseURL + path)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			return resp.StatusCode, path
		}
	}
	return 0, ""
}

// assertScreenServesNoRows fails when a generated SSR screen renders a data
// table for an entity whose REST list route refused the same anonymous caller.
// The screen may answer 200, since it renders an access notice, but it must not
// contain the rows.
func assertScreenServesNoRows(t *testing.T, name, baseURL, table string) {
	t.Helper()
	resp, err := bootProbeClient.Get(baseURL + "/" + table)
	if err != nil {
		t.Fatalf("%s: GET /%s failed: %v — a screen that cannot be reached is not evidence that it is safe", name, table, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		// No screen for this entity at all, so there is nothing that could
		// leak. This is a silent pass by design. Unlike the zero-`checked`
		// case above, "no screen exists" is itself the safe answer.
		return
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusFound,
		resp.StatusCode == http.StatusSeeOther:
		return // refused or redirected to sign in, which is also correct
	case resp.StatusCode >= 500:
		t.Fatalf("%s: GET /%s = %d — a crashing screen is not a passing gate", name, table, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		t.Fatalf("%s: GET /%s = %d — unexpected status, decide deliberately whether it is a refusal", name, table, resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// The markup only: an inlined head's sheets name the marker too.
	if _, markup, _ := strings.Cut(string(body), "<body"); strings.Contains(markup, `data-cui-comp="ui-data-table"`) {
		t.Fatalf("%s: GET /%s renders a data table while GET /api/%s refuses the same anonymous caller — "+
			"a server-rendered screen is a second door to rows the API declines to serve", name, table, table)
	}
	// The check above names one renderer, so it only catches a leak shaped
	// like a data table; a screen that listed the same rows as cards would
	// pass it. Require the refusal POSITIVELY instead: the screen has to say
	// it declined, which no renderer of actual rows does.
	if !strings.Contains(string(body), entityui.AccessDeniedTitle.English()) {
		t.Fatalf("%s: GET /%s returned 200 without the access notice while GET /api/%s refuses the same "+
			"anonymous caller — a screen for refused rows must render the refusal, not merely omit a table",
			name, table, table)
	}
}

// assertScreenServesRows is the other direction, and the one that was missing.
// The gate only ever checked that a refused entity's screen renders no table,
// so a read guard that was too TIGHT was invisible to CI: four shipped
// app specs began rendering nothing but access notices on their public pages
// and every test stayed green.
//
// If the REST list route serves an anonymous caller, the screen for the same
// entity must serve them too, and must show the same rows. Asserting only
// that the refusal notice is ABSENT would pass for a screen that renders an
// empty table because its query broke, which looks identical to a correctly
// permitted screen with nothing to show. restPath is the route that already
// answered 200, so its own payload supplies the expected values.
// Returns (restHadRows, valueCompared): whether the REST route served any rows
// at all, and whether one of their values was matched against the screen.
//
// The two must be able to DIVERGE, or the caller's accounting is dead code.
// An earlier version derived both from the same return points, so they were
// always equal and the guard that watches for "rows existed but nothing was
// compared" could never fire: an anti-vacuity check that was itself vacuous.
// The REST payload is therefore read FIRST, independently of what the screen
// does: a screen that 404s or redirects returns (restHadRows, false), which is
// exactly the divergence the caller needs to see.
func assertScreenServesRows(t *testing.T, name, baseURL, table, restPath string) (bool, bool) {
	t.Helper()
	// Whatever the API serves anonymously, the screen must show. Values come
	// from the live response rather than a hardcoded fixture, so this works
	// across every app spec without knowing what any of them seeded.
	restHadRows, want := firstRowDisplayValues(t, baseURL, restPath)

	resp, err := bootProbeClient.Get(baseURL + "/" + table)
	if err != nil {
		// Matching assertScreenServesNoRows: a screen we could not reach is
		// not evidence either way, and swallowing the error would let a
		// half-dead app pass both directions of the gate silently.
		t.Fatalf("%s: GET /%s failed: %v — an unreachable screen is not evidence that the gate is right", name, table, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		t.Fatalf("%s: GET /%s = %d — a crashing screen is not a passing gate", name, table, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		// No screen at /<table> for this entity (meridian's plans catalog is
		// read over REST by the pricing page and has no list screen of its
		// own). Nothing here can be too tight, so the entity does not count
		// toward the served-rows side of the anti-vacuity check either.
		return false, false
	case resp.StatusCode != http.StatusOK:
		// A sign-in redirect on an entity the API serves anonymously is a
		// legitimate shape this check has no opinion on, but the rows were
		// served, so the caller's counters still see a screen that showed
		// nothing.
		return restHadRows, false
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), entityui.AccessDeniedTitle.English()) {
		t.Fatalf("%s: GET /%s renders a permission notice while GET /api/%s serves the same anonymous caller — "+
			"the read gate is tighter than the entity's declared posture", name, table, table)
	}
	if !restHadRows {
		return false, false // the table is empty; there is nothing the screen could show
	}
	if len(want) == 0 {
		// Rows exist, but the first one carries no comparable display value:
		// every field is numeric, boolean, null, or an id/timestamp this gate
		// skips on purpose. Row presence is still true and the caller's
		// counters still diverge if the screen showed nothing, but there is no
		// value assertion to make here.
		return true, false
	}
	for _, v := range want {
		if strings.Contains(string(body), v) || strings.Contains(string(body), html.EscapeString(v)) {
			return true, true
		}
	}
	t.Fatalf("%s: GET /%s renders no value from the first row %s serves (%q) — the screen is permitted but shows no data",
		name, table, restPath, want)
	return true, false
}

// truncateForLog bounds a response body in a failure message.
func truncateForLog(b []byte) string {
	const max = 400
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}

// firstRowDisplayValues reports whether the list route served any row, and the
// non-empty string values of the first one. Ids and timestamps are skipped:
// those are either hidden on a screen or formatted differently, so matching on
// them would be noise.
//
// The two returns are separate because they answer different questions, and
// deriving presence from the values conflated them. A first row whose fields
// are all numeric, boolean, or null yields no values, and the caller then read
// that as "the table is empty" and skipped the screen assertion entirely, a
// populated table silently opting out of the check.
func firstRowDisplayValues(t *testing.T, baseURL, restPath string) (bool, []string) {
	t.Helper()
	resp, err := bootProbeClient.Get(baseURL + restPath)
	if err != nil {
		// The caller already reached this route once; failing now is a real
		// fault, not a reason to skip the value check. Returning nil here made
		// the whole "screen serves rows" direction evaporate silently.
		t.Fatalf("GET %s failed on the second read: %v", restPath, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	// Status before shape. This function issues the FIRST request to restPath
	// (its caller has not read it yet), so the status is unknown until now and
	// and a 500 body is HTML, which the decode below reports as "a body this
	// gate cannot parse". That message sends the reader to the envelope
	// format when the real fault is the route.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d:\n%s", restPath, resp.StatusCode, truncateForLog(raw))
	}
	// Decode loosely first: a renamed or restructured envelope unmarshals
	// cleanly into a typed struct and leaves Data empty, which is
	// indistinguishable from an empty table, and that is exactly how this
	// gate's value-level half can silently stop verifying anything. Require
	// the `data` key to exist.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("GET %s returned a body this gate cannot parse (%v):\n%s", restPath, err, truncateForLog(raw))
	}
	rawData, ok := envelope["data"]
	if !ok {
		t.Fatalf("GET %s returned an envelope with no \"data\" key — the list response shape changed and this gate can no longer read it:\n%s",
			restPath, truncateForLog(raw))
	}
	var rows []map[string]any
	if err := json.Unmarshal(rawData, &rows); err != nil {
		t.Fatalf("GET %s: \"data\" is not a list of rows (%v):\n%s", restPath, err, truncateForLog(raw))
	}
	if len(rows) == 0 {
		return false, nil // genuinely empty table
	}
	var out []string
	for k, v := range rows[0] {
		if isIDOrTimestampKey(k) {
			continue
		}
		if sv, ok := v.(string); ok && sv != "" {
			out = append(out, sv)
		}
	}
	return true, out
}

// isIDOrTimestampKey matches both key conventions an app spec can serve. The
// framework emits camelCase, but the filter has to hold for a snake_case
// payload too, and checking only `Id`/`At` let `user_id` and `created_at`
// through as display values. Matching a screen on a raw id is noise at best
// and a false pass at worst.
//
// The suffixes are matched with their separator rather than case-folded, so
// "valid", "format" and "seat" stay display values.
func isIDOrTimestampKey(k string) bool {
	return k == "id" ||
		strings.HasSuffix(k, "Id") || strings.HasSuffix(k, "At") ||
		strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_at")
}

// mcpErrorMessage returns the top-level JSON-RPC error message and whether the
// call failed at all.
//
// Scanning the whole response for phrases like "access denied" classified row
// CONTENT as an authorization refusal: a seeded row whose text happens to
// contain the phrase made a successful call read as refused, which is exactly
// the direction that hides a real MCP bypass: the REST route refuses, the MCP
// tool serves the rows, and the parity check calls them equal.
//
// The transport may frame the response as SSE because the request accepts
// text/event-stream, so a body that is not plain JSON is scanned for its data
// frames before giving up.
func mcpErrorMessage(body string) (string, bool) {
	var env struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err == nil {
		if env.Error == nil {
			return "", false
		}
		return env.Error.Message, true
	}
	for _, line := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
		if !ok {
			continue
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &env); err != nil {
			continue
		}
		if env.Error == nil {
			return "", false
		}
		return env.Error.Message, true
	}
	return "", false
}

func postMCP(t *testing.T, baseURL, payload string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", baseURL+"/mcp", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build /mcp request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := bootProbeClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

// bootProbeClient bounds every readiness probe in bootGeneratedApp. A raw
// http.Get has no timeout: an app that accepts the connection but never
// responds would block the probe forever and the 90s deadline below would
// never fire. Five seconds is generous for a first response and well under
// the deadline, so each hung attempt costs one bounded retry, not the gate.
var bootProbeClient = &http.Client{Timeout: 5 * time.Second}

// bootPortAttempts bounds how many fresh ports bootGeneratedApp tries.
// freeAddr closes its probe listener before the child binds, so a
// parallel subtest can take the port in between and the child dies on
// "address already in use". CI hit that on the real-estate app spec.
const bootPortAttempts = 3

// bootGeneratedApp starts a generated app binary on a free port and waits
// for it to serve. A process that exits first (seed validation, failed
// migration, refused bind) fails immediately with its captured output,
// faster than burning the whole HTTP deadline, and the output is the
// diagnostic that matters. A bind lost to another test is the one exit
// retried, on a fresh port and a fresh database. The database is the
// boot's own, in the test's temp dir, never the app's tree, which other
// tests boot too; dbPath names it for tests that read it back.
func bootGeneratedApp(t *testing.T, name, bin, appDir string) (baseURL string, output *clitest.SyncBuffer, dbPath string) {
	t.Helper()
	return bootGeneratedAppOn(t, name, bin, appDir, clitest.FreeAddr)
}

func bootGeneratedAppOn(t *testing.T, name, bin, appDir string, nextAddr func(*testing.T) string) (string, *clitest.SyncBuffer, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "boot-gate.db")
	for attempt := 1; ; attempt++ {
		baseURL, output, lostPort := bootGeneratedAppOnce(t, name, bin, appDir, dbPath, nextAddr(t))
		if !lostPort {
			return baseURL, output, dbPath
		}
		if attempt == bootPortAttempts {
			t.Fatalf("generated %s app lost its port %d times:\n%s", name, attempt, output.String())
		}
		t.Logf("%s: port taken before the app bound it; retrying on a fresh port", name)
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(dbPath + suffix)
		}
	}
}

// portTaken reports a child that exited because its address was bound:
// EADDRINUSE on Unix, WSAEADDRINUSE on Windows.
func portTaken(output string) bool {
	return strings.Contains(output, "address already in use") ||
		strings.Contains(output, "Only one usage of each socket address")
}

// bootGeneratedAppOnce boots the app on addr. lostPort reports that the
// child exited because addr was already bound.
func bootGeneratedAppOnce(t *testing.T, name, bin, appDir, dbPath, addr string) (baseURL string, output *clitest.SyncBuffer, lostPort bool) {
	t.Helper()
	cmd := exec.Command(clitest.TestExecutablePath(bin))
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(),
		"PORT="+addr,
		// A fresh database per boot, so a boot never inherits a stale,
		// already-seeded database from an earlier test or run.
		"DATABASE_URL=file:"+dbPath,
		// The scratch dir lives inside the repo module, so when the repo is
		// checked out as a linked git worktree, framework/isolation activates
		// by default and deterministically remaps the listen port — the app
		// then serves on the remapped port while this probe polls the
		// assigned one until the 90s deadline. The test assigns its own free
		// port, so isolation buys it nothing: pin it off.
		"GOFASTR_ISOLATION=off",
	)
	clitest.ConfigureTestProcessGroup(cmd)
	// syncBuffer: exerciseGeneratedApp reads this while the app is running,
	// and os/exec copies a child's output from its own goroutines until Wait.
	output = &clitest.SyncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start generated %s app: %v", name, err)
	}
	t.Cleanup(func() {
		_ = clitest.KillTestProcessTree(cmd)
		_, _ = cmd.Process.Wait()
	})
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	baseURL = "http://" + addr
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			if portTaken(output.String()) {
				return "", output, true
			}
			t.Fatalf("generated %s app exited before serving (err=%v):\n%s", name, err, output.String())
		default:
		}
		resp, err := bootProbeClient.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			// Any non-server-error answer proves the app is up and routing:
			// auth-gated apps answer 302/401 on /, a screen-less one 404.
			if resp.StatusCode < 500 {
				return baseURL, output, false
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("generated %s app did not serve at %s within 90s:\n%s", name, baseURL, output.String())
	return "", nil, false
}

// TestPortTakenMatchesBothOSes pins the lost-port match on the Unix and
// the Windows wording, and that another early exit is not one.
func TestPortTakenMatchesBothOSes(t *testing.T) {
	for out, want := range map[string]bool{
		"listen tcp 127.0.0.1:39815: bind: address already in use":                                                                       true,
		"listen tcp 127.0.0.1:39815: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.": true,
		"seed: validation failed": false,
	} {
		if got := portTaken(out); got != want {
			t.Errorf("portTaken(%q) = %v, want %v", out, got, want)
		}
	}
}

// TestBootRetriesOnTakenPort pins the retry: the first port handed out
// is held by another listener, so the child exits on "address already
// in use", and the boot must succeed on the next port instead of
// failing the test.
func TestBootRetriesOnTakenPort(t *testing.T) {
	dir := t.TempDir()
	src := "package main\n\nimport (\n\t\"log\"\n\t\"net/http\"\n\t\"os\"\n)\n\n" +
		"func main() {\n\tlog.Fatal(http.ListenAndServe(os.Getenv(\"PORT\"), http.NotFoundHandler()))\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bootprobe\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bootprobe")
	build := exec.Command("go", "build", "-o", clitest.TestExecutablePath(bin), ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v\n%s", err, out)
	}

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	calls := 0
	next := func(t *testing.T) string {
		calls++
		if calls == 1 {
			return held.Addr().String()
		}
		return clitest.FreeAddr(t)
	}
	baseURL, _, _ := bootGeneratedAppOn(t, "bootprobe", bin, dir, next)
	if calls != 2 || baseURL == "http://"+held.Addr().String() {
		t.Fatalf("calls = %d, baseURL = %s; want a second port after the held one", calls, baseURL)
	}
}

// TestBootProbeClientTimesOut pins the readiness probe's timeout: a
// generated app that accepts the connection but never responds must fail
// the probe (and let the 90s deadline fire) rather than hanging the test
// forever on an unbounded http.Get.
func TestBootProbeClientTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // accepts, never responds
	}))
	defer srv.Close()
	defer close(release)
	start := time.Now()
	resp, err := bootProbeClient.Get(srv.URL)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("probe returned no error against a hung server — timeout is not bounded")
	}
	if elapsed := time.Since(start); elapsed >= 10*time.Second {
		t.Fatalf("probe took %s against a hung server — the boot gate would never reach its deadline", elapsed)
	}
}

// exampleTree generates one shipped app spec into a scratch package,
// compiles every package it emitted, vets them, and returns the built app
// binary plus the directory it should run from (output_dir aware). It does
// this once per test binary: the boot gate and the lms and portfolio
// security tests boot the same build, each on its own database.
func exampleTree(t *testing.T, appSpecPath, name string) (bin, appDir string) {
	t.Helper()
	src, err := os.ReadFile(appSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", appSpecPath, err)
	}
	dir, _ := clitest.SharedScratchPackage(t, buildGateScratchPkg+name, func(dir, scratchModule string) error {
		return generateAndCompileAppSpec(dir, scratchModule, appSpecPath, string(src), name)
	})
	return filepath.Join(dir, exampleBinName(name)), exampleAppDir(dir, string(src))
}

// exampleBinName is the app binary's name inside the example's scratch
// package; the leading "_" keeps it out of every `./...` walk.
func exampleBinName(name string) string {
	return clitest.TestExecutablePath("_" + name + "-from-app-spec")
}

// exampleAppDir is where the emitted app lives under the scratch package:
// output_dir moves it one level down (examples/ecommerce uses it), so it is
// resolved from the app spec rather than assuming a flat layout.
func exampleAppDir(dir, appSpec string) string {
	if out := clitest.AppSpecOutputDir(appSpec); out != "" {
		return filepath.Join(dir, out)
	}
	return dir
}

// generateAndCompileAppSpec fills the scratch package dir (import path
// scratchModule) from one app spec and compiles it: the main package,
// every package, and go vet.
func generateAndCompileAppSpec(dir, scratchModule, appSpecPath, src, name string) error {
	// Repoint app.module at the scratch package so the generated self-imports
	// resolve to the code just emitted rather than to whatever package is
	// committed next door.
	realModule := "github.com/DonaldMurillo/gofastr/examples/" + name
	moduleLine := "module: " + realModule
	if !strings.Contains(src, moduleLine) {
		return fmt.Errorf("%s no longer declares %q — update this test's rewrite", appSpecPath, moduleLine)
	}
	rewritten := strings.Replace(src, moduleLine, "module: "+scratchModule, 1)
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(rewritten), 0o644); err != nil {
		return fmt.Errorf("write scratch app spec: %w", err)
	}

	// Generate with the in-tree CLI source, so a generator regression fails
	// here rather than at the next release. The shared test binary IS that
	// source, built once per package run (testbin_test.go); `go run` here
	// re-linked the whole CLI per example.
	cli, err := clitest.GofastrTestBinary()
	if err != nil {
		return fmt.Errorf("build gofastr: %w", err)
	}
	gen := exec.Command(cli, "generate", "app", "gofastr.yml")
	gen.Dir = dir
	if out, err := gen.CombinedOutput(); err != nil {
		return fmt.Errorf("gofastr generate app gofastr.yml: %w\n%s", err, out)
	}
	appDir := exampleAppDir(dir, src)

	// Compile the main package. This pulls in the generated entities package
	// transitively; ./... below covers any package the main package does
	// not import.
	build := exec.Command("go", "build", "-o", filepath.Join(dir, exampleBinName(name)), ".")
	build.Dir = appDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("generated app does not compile — the generator emitted broken Go for %s:\n%s", appSpecPath, out)
	}

	rest := exec.Command("go", "build", "./...")
	rest.Dir = appDir
	if out, err := rest.CombinedOutput(); err != nil {
		return fmt.Errorf("generated packages do not compile for %s:\n%s", appSpecPath, out)
	}

	// `go vet` reaches the emitted _test.go files, which `go build` never
	// compiles. The generator ships e2e/axe tests; broken ones are the same
	// class of defect as broken app code and should fail the same way.
	vet := exec.Command("go", "vet", "./...")
	vet.Dir = appDir
	if out, err := vet.CombinedOutput(); err != nil {
		return fmt.Errorf("generated code fails go vet for %s:\n%s", appSpecPath, out)
	}
	return nil
}

// vetWithRepoVettool is the generated-code gate, the app spec sibling of
// TestGeneratedCLIPassesRepoVettool: the project emitted from a shipped
// app spec must pass the repo's own vettool, the whole module, `_test.go`
// files included. go vet above proves the emitted Go is sound under the
// stdlib analyzers; this adds the repo's analyzer set, so a template
// regression that compiles but trips a repo rule (an unbounded response
// read, a client with no deadline) fails here instead of in every app the
// generator touches. Every diagnostic is a finding in the emitted
// templates (cmd/gofastr/internal/appgen), not in the app spec. Offline
// (GOPROXY=off): a module-resolution failure skips with a message. The
// boot test runs it as its own subtest, so that skip (or its failure)
// leaves the boot alone.
//
//gofastr:test-reads internal/analyzers/ cmd/vettool/
func vetWithRepoVettool(t *testing.T, appDir string) {
	t.Helper()
	vettool, err := clitest.RepoVettool()
	if err != nil {
		t.Fatal(err)
	}
	vet := exec.Command("go", "vet", "-vettool="+vettool, "./...")
	vet.Dir = appDir
	vet.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := vet.CombinedOutput()
	if err == nil {
		return
	}
	text := string(out)
	for _, marker := range []string{"cannot find module", "finding module for package", "dial tcp", "module lookup disabled"} {
		if strings.Contains(text, marker) {
			t.Skipf("generated module could not be built in this environment: %s", text)
		}
	}
	t.Fatalf("GATE: generated project does not pass the repo vettool — every diagnostic is a finding in the emitted templates (cmd/gofastr/internal/appgen), not in the app spec or the customer's code:\n%s", text)
}
