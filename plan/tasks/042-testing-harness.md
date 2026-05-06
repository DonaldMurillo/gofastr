# 042 — Testing Harness

**Phase:** 5 (Testing & Integration) | **Depends on:** 003, 004

## Goal
In-memory test harness. Make requests against app without real HTTP server. Fast.

## Deliverables
- [ ] `gofastr.Test(t, app)` returns `TestClient`
- [ ] `TestClient.Get(path)` → `TestResponse`
- [ ] `TestClient.Post(path, body)` → `TestResponse`
- [ ] `TestClient.Put(path, body)` → `TestResponse`
- [ ] `TestClient.Delete(path)` → `TestResponse`
- [ ] `TestClient.AsUser(user)` → chainable, sets auth context
- [ ] `TestClient.WithHeader(key, value)` → chainable
- [ ] `TestResponse.AssertStatus(t, expected)`
- [ ] `TestResponse.AssertJSON(t, expected)` — deep equality
- [ ] `TestResponse.AssertHeader(t, key, value)`
- [ ] `TestResponse.Body() []byte`
- [ ] `TestResponse.JSON(v)` — unmarshal into v
- [ ] Uses `httptest.NewRequest` + `http.Handler.ServeHTTP` (no real listener)

## Acceptance Criteria
- Test request executes handler without real HTTP
- AssertStatus panics on mismatch (test failure)
- AssertJSON compares decoded JSON (not string comparison)
- AsUser sets user in context
