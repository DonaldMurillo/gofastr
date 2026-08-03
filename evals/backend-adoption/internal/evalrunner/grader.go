package evalrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	userAEmail    = "owner-a@example.test"
	userAPassword = "AdoptionPass-A-7!" // not-a-secret: throwaway login for a synthetic user the harness creates in a local eval app
	userBEmail    = "owner-b@example.test"
	userBPassword = "AdoptionPass-B-8!" // not-a-secret: throwaway login for a synthetic user the harness creates in a local eval app
	initialTitle  = "Printer smoke signal"
)

type grader struct {
	workspace string
	resultDir string
	dbPath    string
	baseURL   string
	clientA   *http.Client
	clientB   *http.Client
	anon      *http.Client
	checks    []Check
	score     int
	maximum   int
}

func Grade(ctx context.Context, workspace, resultDir string, maintenance bool) PhaseResult {
	result := PhaseResult{GradedAt: time.Now(), Maximum: 100}
	source, tests, deps := WorkspaceMetrics(workspace)
	result.SourceLines, result.TestLines, result.DirectDeps = source, tests, deps

	buildCtx, cancelBuild := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelBuild()
	binary := filepath.Join(resultDir, executableName("candidate-app"))
	buildOut, buildErr := commandOutput(buildCtx, workspace, "go", "build", "-o", binary, ".")
	result.BuildOK = buildErr == nil

	testCtx, cancelTest := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelTest()
	testOut, testErr := commandOutput(testCtx, workspace, "go", "test", "./...")
	result.TestOK = testErr == nil

	g := &grader{
		workspace: workspace,
		resultDir: resultDir,
		dbPath:    filepath.Join(resultDir, "support-desk.db"),
		anon:      &http.Client{Timeout: 5 * time.Second},
	}
	g.add("build", 6, buildErr == nil, truncate(buildOut, buildErr))
	g.add("tests", 4, testErr == nil, truncate(testOut, testErr))
	if buildErr != nil {
		result.Score, result.Maximum, result.Checks = g.score, g.maximum, g.checks
		result.GradeError = "candidate did not build"
		return result
	}

	serverLog := filepath.Join(resultDir, phaseName(maintenance)+"-server.log")
	result.ServerLogPath = serverLog
	server, startErr := startServer(ctx, binary, workspace, g.dbPath, serverLog)
	if startErr != nil {
		g.add("server_start", 90, false, startErr.Error())
		result.Score, result.Maximum, result.Checks = g.score, g.maximum, g.checks
		result.GradeError = startErr.Error()
		return result
	}
	g.baseURL = server.baseURL
	defer server.stop()

	jarA, _ := cookiejar.New(nil)
	jarB, _ := cookiejar.New(nil)
	g.clientA = &http.Client{Jar: jarA, Timeout: 5 * time.Second}
	g.clientB = &http.Client{Jar: jarB, Timeout: 5 * time.Second}

	if maintenance {
		g.gradeMaintenance(ctx, server)
	} else {
		g.gradeInitial(ctx, server)
	}
	result.Score, result.Maximum, result.Checks = g.score, g.maximum, g.checks
	return result
}

func (g *grader) gradeInitial(ctx context.Context, server *runningServer) {
	status, body, _, err := g.request(g.anon, http.MethodGet, "/healthz", "", nil)
	g.add("health", 5, err == nil && status == 200, evidence(status, body, err))

	status, body, headers, err := g.form(g.clientA, "/auth/register", userAEmail, userAPassword)
	cookie := headers.Get("Set-Cookie")
	g.add("register", 5, err == nil && status == 201, evidence(status, body, err))
	g.add("cookie_security", 5,
		strings.Contains(strings.ToLower(cookie), "httponly") &&
			strings.Contains(strings.ToLower(cookie), "samesite="),
		cookie)

	status, body, _, err = g.request(g.anon, http.MethodGet, "/api/tickets", "", nil)
	g.add("anonymous_rest", 5, err == nil && status == 401, evidence(status, body, err))

	payload := `{"title":"` + initialTitle + `","body":"The printer emits smoke."}`
	status, body, _, err = g.request(g.clientA, http.MethodPost, "/api/tickets", payload, jsonHeaders())
	ticketID := findStringField(body, "id")
	g.add("create_ticket", 8, err == nil && status == 201 && ticketID != "" &&
		strings.Contains(body, `"open"`), evidence(status, body, err))

	status, listBody, _, err := g.request(g.clientA, http.MethodGet, "/api/tickets", "", nil)
	g.add("list_ticket", 4, err == nil && status == 200 && strings.Contains(listBody, initialTitle),
		evidence(status, listBody, err))

	status, getBody, _, err := g.request(g.clientA, http.MethodGet, "/api/tickets/"+ticketID, "", nil)
	g.add("get_ticket", 4, err == nil && status == 200 && strings.Contains(getBody, initialTitle),
		evidence(status, getBody, err))

	status, patchBody, _, err := g.request(g.clientA, http.MethodPatch, "/api/tickets/"+ticketID,
		`{"status":"closed"}`, jsonHeaders())
	g.add("patch_ticket", 4, err == nil && status == 200 && strings.Contains(patchBody, `"closed"`),
		evidence(status, patchBody, err))

	status, body, _, err = g.request(g.clientA, http.MethodPost, "/api/tickets",
		`{"title":"Cross-site should fail","body":"blocked"}`,
		map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"})
	g.add("cross_site_rejection", 5, err == nil && status == 403, evidence(status, body, err))

	status, body, _, err = g.form(g.clientB, "/auth/register", userBEmail, userBPassword)
	registeredB := err == nil && status == 201
	statusGet, foreignBody, _, foreignErr := g.request(g.clientB, http.MethodGet, "/api/tickets/"+ticketID, "", nil)
	statusPatch, patchForeignBody, _, patchForeignErr := g.request(g.clientB, http.MethodPatch,
		"/api/tickets/"+ticketID, `{"status":"open"}`, jsonHeaders())
	statusList, foreignList, _, listErr := g.request(g.clientB, http.MethodGet, "/api/tickets", "", nil)
	g.add("owner_isolation_rest", 15, registeredB &&
		statusGet == 404 && foreignErr == nil &&
		statusPatch == 404 && patchForeignErr == nil &&
		statusList == 200 && listErr == nil && !strings.Contains(foreignList, initialTitle),
		fmt.Sprintf("register=%t get=%s patch=%s list=%s", registeredB,
			evidence(statusGet, foreignBody, foreignErr),
			evidence(statusPatch, patchForeignBody, patchForeignErr),
			evidence(statusList, foreignList, listErr)))

	g.gradeDiscoveryAndMCP(ticketID, 15)

	status, dashboard, _, err := g.request(g.clientA, http.MethodGet, "/", "", nil)
	g.add("server_rendered_dashboard", 5, err == nil && status == 200 &&
		strings.Contains(dashboard, initialTitle), evidence(status, dashboard, err))

	server.stop()
	restarted, restartErr := startServer(ctx, server.binary, g.workspace, g.dbPath,
		filepath.Join(g.resultDir, "initial-restart-server.log"))
	if restartErr == nil {
		defer restarted.stop()
		g.baseURL = restarted.baseURL
		jar, _ := cookiejar.New(nil)
		g.clientA = &http.Client{Jar: jar, Timeout: 5 * time.Second}
		loginStatus, loginBody, _, loginErr := g.form(g.clientA, "/auth/login", userAEmail, userAPassword)
		getStatus, persistedBody, _, getErr := g.request(g.clientA, http.MethodGet,
			"/api/tickets/"+ticketID, "", nil)
		g.add("restart_persistence", 5, loginStatus == 200 && loginErr == nil &&
			getStatus == 200 && getErr == nil && strings.Contains(persistedBody, initialTitle),
			"login="+evidence(loginStatus, loginBody, loginErr)+" get="+evidence(getStatus, persistedBody, getErr))
	} else {
		g.add("restart_persistence", 5, false, restartErr.Error())
	}

	g.add("password_not_plaintext", 5, !databaseContains(g.dbPath, userAPassword),
		"scanned SQLite database files for the registration password")
}

func (g *grader) gradeDiscoveryAndMCP(ticketID string, points int) {
	status, spec, _, err := g.request(g.anon, http.MethodGet, "/openapi.json", "", nil)
	g.add("openapi", 5, err == nil && status == 200 && strings.Contains(spec, "/api/tickets"),
		evidence(status, spec, err))

	listRequest := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	status, tools, _, err := g.request(g.clientA, http.MethodPost, "/mcp", listRequest, jsonHeaders())
	hasTools := strings.Contains(tools, "tickets_list") &&
		strings.Contains(tools, "tickets_get") &&
		strings.Contains(tools, "tickets_create")
	g.add("mcp_discovery", 5, err == nil && status == 200 && hasTools, evidence(status, tools, err))

	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tickets_list","arguments":{}}}`
	status, ownerMCP, _, err := g.request(g.clientA, http.MethodPost, "/mcp", call, jsonHeaders())
	anonStatus, anonMCP, _, anonErr := g.request(g.anon, http.MethodPost, "/mcp", call, jsonHeaders())
	foreignStatus, foreignMCP, _, foreignErr := g.request(g.clientB, http.MethodPost, "/mcp", call, jsonHeaders())
	mcpSecure := err == nil && status == 200 && strings.Contains(ownerMCP, initialTitle) &&
		anonErr == nil && anonStatus < 500 && !strings.Contains(anonMCP, initialTitle) &&
		foreignErr == nil && foreignStatus < 500 && !strings.Contains(foreignMCP, initialTitle)
	remaining := points - 10
	g.add("mcp_auth_and_owner_isolation", remaining, mcpSecure,
		fmt.Sprintf("owner=%s anonymous=%s other_user=%s ticket=%s", evidence(status, ownerMCP, err),
			evidence(anonStatus, anonMCP, anonErr), evidence(foreignStatus, foreignMCP, foreignErr), ticketID))
}

func (g *grader) gradeMaintenance(ctx context.Context, server *runningServer) {
	status, health, _, err := g.request(g.anon, http.MethodGet, "/healthz", "", nil)
	g.add("regression_health", 5, err == nil && status == 200, evidence(status, health, err))

	loginA, loginBody, _, loginErr := g.form(g.clientA, "/auth/login", userAEmail, userAPassword)
	loginB, loginBBody, _, loginBErr := g.form(g.clientB, "/auth/login", userBEmail, userBPassword)
	g.add("regression_login", 5, loginA == 200 && loginErr == nil && loginB == 200 && loginBErr == nil,
		"a="+evidence(loginA, loginBody, loginErr)+" b="+evidence(loginB, loginBBody, loginBErr))

	initialID := initialTicketID(g.resultDir)
	initialPath := "/api/tickets?limit=100"
	if initialID != "" {
		initialPath = "/api/tickets/" + initialID
	}
	status, existing, _, err := g.request(g.clientA, http.MethodGet, initialPath, "", nil)
	g.add("migration_preserves_existing", 15, err == nil && status == 200 &&
		strings.Contains(existing, initialTitle) && strings.Contains(existing, `"priority"`) &&
		strings.Contains(existing, `"normal"`), evidence(status, existing, err))

	highTitle := "Urgent toner eruption"
	status, created, _, err := g.request(g.clientA, http.MethodPost, "/api/tickets",
		`{"title":"`+highTitle+`","body":"Please hurry","priority":"high"}`, jsonHeaders())
	highID := findStringField(created, "id")
	g.add("priority_create", 10, err == nil && status == 201 && highID != "" &&
		strings.Contains(created, `"high"`), evidence(status, created, err))

	status, patched, _, err := g.request(g.clientA, http.MethodPatch, "/api/tickets/"+highID,
		`{"priority":"low"}`, jsonHeaders())
	g.add("priority_patch", 8, err == nil && status == 200 && strings.Contains(patched, `"low"`),
		evidence(status, patched, err))

	status, invalid, _, err := g.request(g.clientA, http.MethodPatch, "/api/tickets/"+highID,
		`{"priority":"urgent"}`, jsonHeaders())
	getStatus, afterInvalid, _, getErr := g.request(g.clientA, http.MethodGet, "/api/tickets/"+highID, "", nil)
	g.add("invalid_priority_atomic", 7, err == nil && status == 400 && getErr == nil &&
		getStatus == 200 && strings.Contains(afterInvalid, `"low"`) &&
		!strings.Contains(afterInvalid, `"urgent"`), evidence(status, invalid, err)+" after="+
		evidence(getStatus, afterInvalid, getErr))

	status, spec, _, err := g.request(g.anon, http.MethodGet, "/openapi.json", "", nil)
	g.add("priority_openapi", 5, err == nil && status == 200 &&
		strings.Contains(spec, "/api/tickets") && strings.Contains(spec, "priority"),
		evidence(status, spec, err))

	call := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"tickets_list","arguments":{}}}`
	status, ownerMCP, _, err := g.request(g.clientA, http.MethodPost, "/mcp", call, jsonHeaders())
	foreignStatus, foreignMCP, _, foreignErr := g.request(g.clientB, http.MethodPost, "/mcp", call, jsonHeaders())
	g.add("priority_mcp_and_isolation", 12, err == nil && status == 200 &&
		strings.Contains(ownerMCP, "priority") && strings.Contains(ownerMCP, highTitle) &&
		foreignErr == nil && foreignStatus < 500 && !strings.Contains(foreignMCP, highTitle),
		"owner="+evidence(status, ownerMCP, err)+" other="+evidence(foreignStatus, foreignMCP, foreignErr))

	status, dashboard, _, err := g.request(g.clientA, http.MethodGet, "/", "", nil)
	g.add("priority_dashboard", 5, err == nil && status == 200 &&
		strings.Contains(dashboard, highTitle) &&
		(strings.Contains(strings.ToLower(dashboard), "low") ||
			strings.Contains(strings.ToLower(dashboard), "priority")),
		evidence(status, dashboard, err))

	status, csrfBody, _, err := g.request(g.clientA, http.MethodPost, "/api/tickets",
		`{"title":"Cross-site maintenance","body":"blocked","priority":"high"}`,
		map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"})
	g.add("regression_cross_site", 5, err == nil && status == 403, evidence(status, csrfBody, err))

	status, foreign, _, err := g.request(g.clientB, http.MethodGet, "/api/tickets/"+highID, "", nil)
	patchStatus, patchForeign, _, patchErr := g.request(g.clientB, http.MethodPatch,
		"/api/tickets/"+highID, `{"status":"open"}`, jsonHeaders())
	g.add("regression_owner_isolation", 8, err == nil && status == 404 &&
		patchErr == nil && patchStatus == 404,
		"get="+evidence(status, foreign, err)+" patch="+evidence(patchStatus, patchForeign, patchErr))

	server.stop()
	restarted, restartErr := startServer(ctx, server.binary, g.workspace, g.dbPath,
		filepath.Join(g.resultDir, "maintenance-restart-server.log"))
	if restartErr == nil {
		defer restarted.stop()
		g.baseURL = restarted.baseURL
		jar, _ := cookiejar.New(nil)
		g.clientA = &http.Client{Jar: jar, Timeout: 5 * time.Second}
		loginStatus, _, _, loginErr := g.form(g.clientA, "/auth/login", userAEmail, userAPassword)
		getStatus, persisted, _, getErr := g.request(g.clientA, http.MethodGet, "/api/tickets/"+highID, "", nil)
		g.add("regression_restart_persistence", 5, loginStatus == 200 && loginErr == nil &&
			getStatus == 200 && getErr == nil && strings.Contains(persisted, `"low"`),
			evidence(getStatus, persisted, getErr))
	} else {
		g.add("regression_restart_persistence", 5, false, restartErr.Error())
	}
}

func (g *grader) add(id string, maximum int, passed bool, evidenceText string) {
	points := 0
	if passed {
		points = maximum
	}
	g.score += points
	g.maximum += maximum
	g.checks = append(g.checks, Check{
		ID: id, Points: points, Maximum: maximum, Passed: passed, Evidence: evidenceText,
	})
}

func (g *grader) form(client *http.Client, path, email, password string) (int, string, http.Header, error) {
	form := url.Values{"email": {email}, "password": {password}}
	return g.request(client, http.MethodPost, path, form.Encode(),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

func (g *grader) request(client *http.Client, method, path, body string, headers map[string]string) (int, string, http.Header, error) {
	req, err := http.NewRequest(method, g.baseURL+path, strings.NewReader(body))
	if err != nil {
		return 0, "", nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return resp.StatusCode, string(data), resp.Header.Clone(), readErr
}

type runningServer struct {
	cmd     *exec.Cmd
	binary  string
	baseURL string
	done    chan error
}

func startServer(ctx context.Context, binary, workspace, dbPath, logPath string) (*runningServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary)
	configureCommandCancellation(cmd)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "PORT="+addr, "DATABASE_PATH="+dbPath)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	server := &runningServer{
		cmd: cmd, binary: binary, baseURL: "http://" + addr, done: make(chan error, 1),
	}
	go func() {
		server.done <- cmd.Wait()
		_ = logFile.Close()
	}()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, requestErr := client.Get(server.baseURL + "/healthz")
		if requestErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return server, nil
			}
		}
		select {
		case waitErr := <-server.done:
			return nil, fmt.Errorf("server exited before ready: %v", waitErr)
		case <-time.After(100 * time.Millisecond):
		}
	}
	server.stop()
	return nil, fmt.Errorf("server did not become ready at %s (log: %s)", server.baseURL, logPath)
}

func (s *runningServer) stop() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	killCommandTree(s.cmd)
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		_ = s.cmd.Process.Kill()
	}
	s.cmd = nil
}

func commandOutput(ctx context.Context, dir, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	configureCommandCancellation(cmd)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return output.String(), err
}

func findStringField(body, key string) string {
	var value any
	if json.Unmarshal([]byte(body), &value) != nil {
		return ""
	}
	var walk func(any) string
	walk = func(current any) string {
		switch typed := current.(type) {
		case map[string]any:
			if raw, ok := typed[key].(string); ok && raw != "" {
				return raw
			}
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(value)
}

func databaseContains(path, needle string) bool {
	matches, _ := filepath.Glob(path + "*")
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err == nil && bytes.Contains(data, []byte(needle)) {
			return true
		}
	}
	return false
}

func jsonHeaders() map[string]string {
	return map[string]string{"Content-Type": "application/json"}
}

func evidence(status int, body string, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("status=%d body=%q", status, truncateText(body, 320))
}

func truncate(output string, err error) string {
	if err != nil {
		return fmt.Sprintf("%v: %s", err, truncateText(output, 500))
	}
	return truncateText(output, 500)
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func phaseName(maintenance bool) string {
	if maintenance {
		return "maintenance"
	}
	return "initial"
}

func initialTicketID(resultDir string) string {
	data, err := os.ReadFile(filepath.Join(resultDir, "initial-grade.json"))
	if err != nil {
		return ""
	}
	var phase PhaseResult
	if json.Unmarshal(data, &phase) != nil {
		return ""
	}
	for _, check := range phase.Checks {
		if check.ID != "create_ticket" {
			continue
		}
		_, quoted, ok := strings.Cut(check.Evidence, "body=")
		if !ok {
			return ""
		}
		body, err := strconv.Unquote(quoted)
		if err != nil {
			return ""
		}
		return findStringField(body, "id")
	}
	return ""
}
