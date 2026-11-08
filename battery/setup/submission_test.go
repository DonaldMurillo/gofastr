package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRejectedStepKeepsPublicValues(t *testing.T) {
	for _, validation := range []bool{true, false} {
		t.Run(map[bool]string{true: "validate", false: "run"}[validation], func(t *testing.T) {
			t.Setenv("SETUP_TEST_NAME", "environment")
			t.Setenv("SETUP_TEST_SECRET", "environment-secret")
			name := Field{Name: "name", Label: pageContinue, EnvVar: "SETUP_TEST_NAME"}
			if validation {
				name.Validate = func(string) error { return errors.New("invalid") }
			}
			r := New(Config{DisableToken: true, Complete: func(context.Context) (bool, error) { return false, nil }, Steps: []Step{{
				Name: "profile", Title: pageContinue, Fields: []Field{name, {Name: "empty", Label: pageContinue, EnvVar: "SETUP_TEST_NAME"}, {Name: "password", Label: pageContinue, Secret: true, EnvVar: "SETUP_TEST_SECRET"}},
				Run: func(context.Context, map[string]string) error { return errors.New("step failed") },
			}}})
			h := r.Handler(func() {}, nil, nil)
			w := doPost(h, "/setup", "name=Alice+%26+Bob&empty=&password=private-value", nil)
			body := w.Body.String()
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, body)
			}
			if !strings.Contains(body, `value="Alice &amp; Bob"`) {
				t.Errorf("submitted public value was lost: %s", body)
			}
			if strings.Contains(body, `value="environment"`) {
				t.Error("empty submission replaced by environment")
			}
			for _, secret := range []string{"private-value", "environment-secret"} {
				if strings.Contains(body, secret) {
					t.Errorf("secret echoed: %s", secret)
				}
			}
		})
	}
}
