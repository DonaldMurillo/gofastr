package main

// Property: the lms example blueprint gates every student-record entity
// (enrollments, progress, certificates) the way it gates the student
// roster itself: RBAC on every operation, so an unrelated registered
// account is refused on read and write — the generated app must not
// ship world-open student data.
import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestLmsGatesStudentRecords boots the real lms blueprint, seeds one of
// each student record through the API a staff registrar would use, then
// registers a SECOND, unrelated account and asserts that stranger is refused
// on read and write of all three record families. RED today: the default
// posture lets every session through.
func TestLmsGatesStudentRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, builds, and boots an app")
	}
	bin, appDir := generateAndCompileBlueprint(t, "../../examples/lms/gofastr.yml", "lms")

	// registerAndLogin mints a fresh account exactly the way the portfolio
	baseURL, appOut := bootGeneratedApp(t, "lms", bin, appDir)
	registerAndLogin := func(email string) *http.Client {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		creds := fmt.Sprintf(`{"email":%q,"password":"str0ng-passphrase"}`, email)
		for _, path := range []string{"/auth/register", "/auth/login"} {
			resp, err := client.Post(baseURL+path, "application/json", strings.NewReader(creds))
			if err != nil {
				t.Fatalf("%s %s: %v", path, email, err)
			}
			out, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
				t.Fatalf("%s %s = %d: %s", path, email, resp.StatusCode, out)
			}
		}
		return client
	}

	api := func(client *http.Client, method, path, body string) (int, map[string]any, string) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, baseURL+path, rd)
		if err != nil {
			t.Fatalf("%s %s: build request: %v", method, path, err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m) // envelope or row; callers check shape
		return resp.StatusCode, m, string(raw)
	}

	// The registrar account staff would hold. It is NOT special: any session
	// can do what it does, which is half the finding.
	registrar := registerAndLogin("registrar@learnhub.test")

	// Seed one row per record family, keyed off the blueprint's own seeded
	// rows. students is RBAC-gated so the roster (and therefore the ids the
	// FK columns demand) is read straight from the boot DB the harness
	// assigns — DATABASE_URL=file:<appDir>/boot-gate.db.
	bootDB, err := sql.Open("sqlite3", "file:"+filepath.Join(appDir, "boot-gate.db")+"?mode=ro")
	if err != nil {
		t.Fatalf("open boot db: %v", err)
	}
	t.Cleanup(func() { _ = bootDB.Close() })
	seedID := func(table string) string {
		t.Helper()
		var id string
		if err := bootDB.QueryRow("SELECT id FROM " + table + " LIMIT 1").Scan(&id); err != nil {
			t.Fatalf("seed id for %s: %v", table, err)
		}
		return id
	}
	studentID, courseID, lessonID := seedID("students"), seedID("courses"), seedID("lessons")

	post := func(path, body string) (int, map[string]any) {
		code, m, raw := api(registrar, http.MethodPost, path, body)
		if code == http.StatusForbidden || code == http.StatusUnauthorized {
			// Gated: the posture this test wants. The stranger checks below
			// still run; with create gated they expect refusal too.
			return code, nil
		}
		if code != http.StatusCreated && code != http.StatusOK {
			t.Fatalf("POST %s = %d (setup): %s\napp log:\n%s", path, code, raw, appOut.String())
		}
		return code, m
	}
	rowID := func(row map[string]any) string {
		if row == nil {
			return ""
		}
		if data, ok := row["data"].(map[string]any); ok {
			row = data
		}
		if id, ok := row["id"].(float64); ok {
			return fmt.Sprintf("%.0f", id)
		}
		if id, ok := row["id"].(string); ok {
			return id
		}
		return ""
	}
	enrollmentJSON := fmt.Sprintf(`{"student_id":%q,"course_id":%q,"status":"active"}`, studentID, courseID)
	_, enrollment := post("/api/enrollments", enrollmentJSON)
	progressJSON := fmt.Sprintf(`{"student_id":%q,"lesson_id":%q,"status":"in_progress","score":95,"notes":"instructor notes: strong start"}`, studentID, lessonID)
	_, progress := post("/api/progress", progressJSON)
	certificateJSON := fmt.Sprintf(`{"student_id":%q,"course_id":%q,"enrollment_id":%q,"final_score":95}`, studentID, courseID, rowID(enrollment))
	_, certificate := post("/api/certificates", certificateJSON)

	// The stranger: a freshly registered, role-less account with no relation
	// to any student. Everything it can reach below is the finding.
	stranger := registerAndLogin("stranger@learnhub.test")

	code, _, raw := api(stranger, http.MethodGet, "/api/enrollments", "")
	if code == http.StatusOK && (strings.Contains(raw, "studentId") || strings.Contains(raw, "student_id")) {
		t.Errorf("unrelated account GET /api/enrollments = 200 with rows (%s): every student's enrollment status and progress_percent is readable by any registered account — enrollments has no access block while the same file RBAC-gates students. Want gated refusal (403/404) or an empty result", truncate(raw, 200))
	}

	// 2. Rewriting a student's progress record.
	if pid := rowID(progress); pid != "" {
		code, _, raw = api(stranger, http.MethodPatch, "/api/progress/"+pid, `{"score":0,"notes":"rewritten by stranger"}`)
		if code == http.StatusOK {
			t.Errorf("unrelated account PATCH /api/progress/%s = 200 (%s): any registered account rewrites any student's score and instructor notes. Want 403", pid, truncate(raw, 200))
		}
	} else {
		t.Log("progress row id not captured from create response; PATCH leg skipped (create was gated or shape changed)")
	}

	// 3. Certificate codes.
	code, _, raw = api(stranger, http.MethodGet, "/api/certificates", "")
	if code == http.StatusOK && (strings.Contains(raw, "code") || rowID(certificate) != "") {
		t.Errorf("unrelated account GET /api/certificates = 200 with rows (%s): unique certificate codes are harvestable by any registered account. Want gated refusal or empty result", truncate(raw, 200))
	}
}
