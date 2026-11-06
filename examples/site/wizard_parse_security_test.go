package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWizardRefusesMalformedForm(t *testing.T) {
	app := newTestApp(t)
	for _, body := range []string{"wd_name=%zz", "wizard_action=%G1", "wd_email=%Q2", "wd_name=a;b=c"} {
		t.Run(body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, wizardDemoPath, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			app.Router().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.String() != "invalid form\n" {
				t.Fatalf("malformed form response = %d %q, want constant 400", response.Code, response.Body.String())
			}
		})
	}
}
