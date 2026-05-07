package main

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerResponse(t *testing.T) {
	ds := setupDevServer()
	srv := httptest.NewServer(ds)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	t.Log("Status:", resp.StatusCode)
	t.Log("Content-Type:", resp.Header.Get("Content-Type"))
	t.Log("Body length:", len(body))

	// Check closing tags
	s := string(body)

	// Show all occurrences of "cart-badge"
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], "cart-badge")
		if idx == -1 {
			break
		}
		start := i + idx - 20
		if start < 0 {
			start = 0
		}
		end := i + idx + 50
		if end > len(s) {
			end = len(s)
		}
		t.Logf("cart-badge at %d: ...%s...", i+idx, s[start:end])
		i = i + idx + 10
	}

	// Show all occurrences of "add-to-cart"
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], "add-to-cart")
		if idx == -1 {
			break
		}
		start := i + idx - 20
		if start < 0 {
			start = 0
		}
		end := i + idx + 50
		if end > len(s) {
			end = len(s)
		}
		t.Logf("add-to-cart at %d: ...%s...", i+idx, s[start:end])
		i = i + idx + 11
	}

	if !strings.Contains(s, "</body>") {
		t.Error("missing </body> tag")
	}
	if !strings.Contains(s, "</html>") {
		t.Error("missing </html> tag")
	}

	// Test static file serving for images
	imgReq := httptest.NewRequest("GET", "/img/widget.svg", nil)
	imgW := httptest.NewRecorder()
	ds.ServeHTTP(imgW, imgReq)
	if imgW.Code != 200 {
		t.Errorf("expected image to be served with 200, got %d", imgW.Code)
	}
}
