package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestExampleReadScopeBoots is the end-to-end posture proof on a real shipped
// app spec: an anonymous caller must NOT receive the draft post or the
// unapproved testimonial the portfolio seed writes, and a signed-in caller
// must. It also proves the bool predicate binds: approved eq "true" has to
// match rows stored as boolean true.
//
//gofastr:test-reads examples/portfolio/gofastr.yml
func TestExampleReadScopeBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, builds, and boots an app")
	}
	clitest.VariantBuild(t, "examples/portfolio/gofastr.yml")
	bin, appDir := exampleTree(t, "../../examples/portfolio/gofastr.yml", "portfolio")
	baseURL, _, _ := bootGeneratedApp(t, "portfolio", bin, appDir)

	anon := func(table string) []map[string]any {
		t.Helper()
		resp, err := http.Get(baseURL + "/api/" + table)
		if err != nil {
			t.Fatalf("GET /api/%s: %v", table, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/%s = %d, want 200 (a blank read: serves anonymous callers)", table, resp.StatusCode)
		}
		var envelope struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode /api/%s payload: %v", table, err)
		}
		return envelope.Data
	}

	anonPosts := anon("posts")
	for _, row := range anonPosts {
		if row["status"] == "draft" {
			t.Errorf("anonymous /api/posts served the seeded draft post: %#v", row)
		}
	}
	if len(anonPosts) == 0 {
		t.Fatal("anonymous /api/posts served no rows at all: the filter matched nothing, not just unpublished rows")
	}
	anonTestimonials := anon("testimonials")
	for _, row := range anonTestimonials {
		if row["approved"] == false {
			t.Errorf("anonymous /api/testimonials served the unapproved testimonial: %#v", row)
		}
	}
	if len(anonTestimonials) == 0 {
		t.Fatal("anonymous /api/testimonials served no rows at all: the bool predicate matched nothing")
	}

	// Sign in (any session lifts the filter; Unrestricted is blank here).
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	creds := strings.NewReader(`{"email":"readscope@example.com","password":"str0ng-passphrase"}`)
	regResp, err := client.Post(baseURL+"/auth/register", "application/json", creds)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	regResp.Body.Close()
	loginResp, err := client.Post(baseURL+"/auth/login", "application/json",
		strings.NewReader(`{"email":"readscope@example.com","password":"str0ng-passphrase"}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d, want 200: the signed-in half of this proof cannot run", loginResp.StatusCode)
	}

	authed := func(table string) []map[string]any {
		t.Helper()
		resp, err := client.Get(baseURL + "/api/" + table)
		if err != nil {
			t.Fatalf("authed GET /api/%s: %v", table, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("authed GET /api/%s = %d, want 200", table, resp.StatusCode)
		}
		var envelope struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode authed /api/%s: %v", table, err)
		}
		return envelope.Data
	}

	var sawDraft bool
	for _, row := range authed("posts") {
		if row["status"] == "draft" {
			sawDraft = true
		}
	}
	if !sawDraft {
		t.Error("signed-in /api/posts did not serve the seeded draft post: an empty Unrestricted should lift the filter for any session")
	}
	var sawUnapproved bool
	for _, row := range authed("testimonials") {
		if row["approved"] == false {
			sawUnapproved = true
		}
	}
	if !sawUnapproved {
		t.Error("signed-in /api/testimonials did not serve the seeded unapproved testimonial")
	}
}
