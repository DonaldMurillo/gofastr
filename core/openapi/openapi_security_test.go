package openapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAPI_OperationIDNoSpecialChars verifies that operation IDs
// don't contain dangerous characters. Attack: injection via operation IDs.
func TestOpenAPI_OperationIDNoSpecialChars(t *testing.T) {
	op := NewOperation()
	op.OperationID = "getUser"
	if strings.ContainsAny(op.OperationID, `<>"'&\`) {
		t.Errorf("SECURITY: [openapi] operation ID contains special chars: %q.", op.OperationID)
	}
}

// TestOpenAPI_ParameterNameSafe verifies that parameter names don't
// allow injection. Attack: parameter name injection into spec.
func TestOpenAPI_ParameterNameSafe(t *testing.T) {
	op := NewOperation()
	op.AddParameter("id", "path", "User ID", true, nil)

	if len(op.Parameters) != 1 {
		t.Fatalf("expected 1 parameter")
	}
	name := op.Parameters[0]["name"].(string)
	if strings.ContainsAny(name, `<>"'`) {
		t.Errorf("SECURITY: [openapi] parameter name contains special chars: %q.", name)
	}
}

// TestOpenAPI_GatedHandlerHidesSpec verifies that GatedHandler responds
// with 404 (not 401/403) when the allow predicate denies — leaking the
// existence of an OpenAPI endpoint is the disclosure we want to avoid.
func TestOpenAPI_GatedHandlerHidesSpec(t *testing.T) {
	spec := NewSpec("test", "1.0")
	gated := GatedHandler(spec, func(r *http.Request) bool { return false })

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr := httptest.NewRecorder()
	gated.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("SECURITY: [openapi] denied request returned %d (want 404). Attack: 401/403 reveals the gated endpoint exists.", rr.Code)
	}
	if body := rr.Body.String(); strings.Contains(body, "openapi") || strings.Contains(body, "paths") {
		t.Errorf("SECURITY: [openapi] denied request leaked spec contents: %s", body)
	}
}

// TestOpenAPI_GatedHandlerAdmitsAllowed verifies the allow=true path
// still returns the spec.
func TestOpenAPI_GatedHandlerAdmitsAllowed(t *testing.T) {
	spec := NewSpec("test", "1.0")
	gated := GatedHandler(spec, func(r *http.Request) bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr := httptest.NewRecorder()
	gated.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("allowed request returned %d, want 200", rr.Code)
	}
}

// TestOpenAPI_GatedHandlerNilAllowDenies verifies a nil predicate
// fail-closes — never returns the spec.
func TestOpenAPI_GatedHandlerNilAllowDenies(t *testing.T) {
	spec := NewSpec("test", "1.0")
	gated := GatedHandler(spec, nil)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr := httptest.NewRecorder()
	gated.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("SECURITY: [openapi] nil allow predicate did not fail-close (got %d, want 404). Attack: misconfigured gate accidentally exposes spec.", rr.Code)
	}
}

// TestOpenAPI_ToMapSanitized verifies that ToMap output is safe for
// JSON serialization. Attack: JSON injection via operation fields.
func TestOpenAPI_ToMapSanitized(t *testing.T) {
	op := NewOperation()
	op.Summary = "Get user by ID"
	op.Description = "Returns a single user"
	op.OperationID = "getUser"

	m := op.ToMap()
	if m["operationId"] != "getUser" {
		t.Errorf("operationId not in map: %v", m)
	}
}

func TestOpenAPI_HandlerDoesNotAllowWildcardCORS(t *testing.T) {
	spec := NewSpec("test", "1.0")
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr := httptest.NewRecorder()

	Handler(spec).ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Fatalf("SECURITY: [openapi] handler returned Access-Control-Allow-Origin: *. Attack: any website can read the framework's route/spec inventory cross-origin.")
	}
}

func TestOpenAPI_HandlerCarriesNoStore(t *testing.T) {
	spec := NewSpec("test", "1.0")
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr := httptest.NewRecorder()

	Handler(spec).ServeHTTP(rr, req)

	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SECURITY: [openapi] handler missing Cache-Control no-store: %#v", rr.Header())
	}
}

func TestSwaggerUIHandler_DoesNotLoadThirdPartyCDNAssets(t *testing.T) {
	spec := NewSpec("test", "1.0")
	req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	rr := httptest.NewRecorder()

	SwaggerUIHandler(spec, "/docs").ServeHTTP(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "https://unpkg.com/") {
		t.Fatalf("SECURITY: [openapi] swagger UI loaded third-party CDN assets: %q. Attack: docs page depends on remote JS/CSS supply chain by default.", body)
	}
}

func TestSwaggerUIHandler_CarriesContentSecurityPolicy(t *testing.T) {
	spec := NewSpec("test", "1.0")
	req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	rr := httptest.NewRecorder()

	SwaggerUIHandler(spec, "/docs").ServeHTTP(rr, req)

	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("SECURITY: [openapi] swagger UI missing Content-Security-Policy header: %#v", rr.Header())
	}
}
