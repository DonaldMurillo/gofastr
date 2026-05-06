# Fullstack Framework Security: Research & Best Practices

> Comprehensive security reference for building GoFastr — a security-first Go fullstack framework.
> Compiled from OWASP guidelines, framework source analysis, and production incident post-mortems.

---

## Table of Contents

1. [OWASP Top 10 (2025) — Framework Mitigations](#1-owasp-top-10-2025--framework-mitigations)
2. [Framework-Level Security Features](#2-framework-level-security-features)
3. [How Modern Frameworks Handle Security](#3-how-modern-frameworks-handle-security)
4. [Security-First Architecture for a Go Framework](#4-security-first-architecture-for-a-go-framework)
5. [Security Checklist: Day 1 vs. Later](#5-security-checklist-day-1-vs-later)

---

## 1. OWASP Top 10 (2025) — Framework Mitigations

### 1.1 Broken Access Control

**The threat:** Users acting outside their intended permissions — accessing other users' data, modifying records without authorization, force-browsing to admin endpoints, IDOR (Insecure Direct Object References).

**Real-world impact:** The #1 OWASP vulnerability. Accounts for more breaches than any other category. A single IDOR bug can expose every user's data.

**How frameworks mitigate it:**

| Mechanism | Description | Framework Examples |
|-----------|-------------|-------------------|
| **Route-level guards** | Middleware that checks auth/role before handler executes | Rails `before_action`, Laravel middleware, Django decorators |
| **Resource-level auth** | Verify the *specific* resource belongs to the current user | Rails `current_user.posts.find(params[:id])`, Django `get_object_or_404(owner=request.user)` |
| **RBAC middleware** | Role-Based Access Control at route or controller level | Laravel gates/policies, Django permissions |
| **ABAC policies** | Attribute-Based Access Control — context-aware rules | OPA (Open Policy Agent), Cedar, Casbin |
| **Default-deny** | Everything requires explicit permission; no implicit access | Django's `@login_required`, Phoenix plugs |
| **IDOR prevention** | Use UUIDs or slugs instead of sequential IDs; always scope queries to current user | Framework-level query scoping |

**What GoFastr should do:**
- Require explicit authorization annotations on every route
- Provide middleware for RBAC and resource-level scoping
- Default to deny — unauthenticated = 401, unauthorized = 403
- Offer UUID generation for primary keys to prevent enumeration
- Never trust client-side routing for access control decisions

```go
// Example: Authorization should be declarative and hard to forget
router.GET("/users/:id/profile",
    middleware.RequireAuth(),
    middleware.RequireOwnerOrRole("admin"), // checks :id matches current user OR role
    handlers.GetUserProfile,
)
```

---

### 1.2 Cryptographic Failures

**The threat:** Exposing sensitive data through weak cryptography, missing encryption, transmitting data in cleartext, using deprecated algorithms (MD5, SHA1, DES), hardcoding secrets, improper key management.

**Real-world impact:** Password leaks, PII exposure, compliance violations (GDPR, HIPAA, PCI-DSS).

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **HTTPS enforcement** | Redirect HTTP→HTTPS, set HSTS headers |
| **Secure password hashing** | bcrypt/argon2 built-in; never MD5/SHA for passwords |
| **Encryption at rest** | Encrypted database columns, encrypted file storage |
| **TLS configuration** | Secure cipher suites, TLS 1.2+ minimum, proper cert pinning |
| **Secret management** | Never hardcode; use env vars, vault integration |
| **Sensitive data classification** | Framework-level data masking in logs and error responses |

**What GoFastr should do:**
- Force HTTPS in production (configurable opt-out for local dev)
- Default password hashing to argon2id (Go stdlib `golang.org/x/crypto/argon2`)
- Provide encrypted cookie sessions out of the box
- Auto-strip sensitive fields from logs (passwords, tokens, SSNs)
- Offer AES-256-GCM helpers for encrypting data at rest
- Reject known-insecure TLS configurations at startup

---

### 1.3 Injection (SQL, NoSQL, XSS, LDAP, Command Injection)

**The threat:** Untrusted data sent to an interpreter as part of a command or query. The attacker's hostile data tricks the interpreter into executing unintended commands or accessing unauthorized data.

**Real-world impact:** Database dumps, admin panel takeover, data destruction, RCE (Remote Code Execution).

**How frameworks mitigate it:**

#### SQL Injection
| Mechanism | Description |
|-----------|-------------|
| **Parameterized queries** | The gold standard — separate query structure from data |
| **Query builders** | `WHERE id = ?` style — framework enforces parameterization |
| **ORM safety** | ORMs parameterize by default; raw query escape hatches are the danger zone |
| **Input validation** | Schema validation before data reaches the query layer |

```go
// NEVER do this:
db.Query("SELECT * FROM users WHERE email = '" + email + "'")

// ALWAYS do this:
db.Query("SELECT * FROM users WHERE email = $1", email)

// BETTER: use a query builder that makes injection impossible
db.SelectFrom("users").Where("email", email).One(&user)
```

#### XSS (Cross-Site Scripting)
| Mechanism | Description |
|-----------|-------------|
| **Auto-escaping** | Template engines escape by default (Go `html/template` does this) |
| **CSP headers** | Content Security Policy prevents inline script execution |
| **Trusted Types** | Browser API that enforces safe DOM manipulation |
| **Sanitization** | Strip or encode dangerous HTML before rendering |

#### Command Injection
| Mechanism | Description |
|-----------|-------------|
| **No shell execution** | Use `exec.Command` with args array, never `exec.Command("sh", "-c", userInput)` |
| **Input allowlisting** | Validate against known-safe patterns before passing to system calls |

**What GoFastr should do:**
- Use `html/template` (not `text/template`) for HTML rendering — it auto-escapes
- Provide a query builder that makes string concatenation unnecessary
- Offer `SafeHTML` type that requires explicit opt-in for unescaped output
- Set restrictive CSP headers by default
- Provide command execution helpers that prevent shell injection

---

### 1.4 Insecure Design

**The threat:** Fundamental design flaws — missing threat modeling, no security architecture, insecure defaults, missing rate limiting, no business logic validation.

**Real-world impact:** This is about *missing* security, not *broken* security. The system works as designed, but the design is insecure.

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **Secure defaults** | Everything secure out of the box; devs opt out, not in |
| **Threat modeling guides** | Documentation that walks through common attack scenarios |
| **Reference architectures** | Pre-built patterns for common flows (auth, payments, file upload) |
| **Security linter rules** | Static analysis that flags insecure patterns at build time |
| **Design pattern enforcement** | Middleware ordering guarantees, route naming conventions |

**What GoFastr should do:**
- Ship with a security middleware stack that's ON by default
- Provide reference implementations for: login, registration, password reset, file upload, admin panels
- Include a `gofastr audit` CLI command that checks for common misconfigurations
- Document the threat model for every feature

---

### 1.5 Security Misconfiguration

**The threat:** Default credentials, unnecessary features enabled, overly permissive CORS, verbose error messages in production, missing security headers, unpatched systems.

**Real-world impact:** The most common vulnerability. Almost every breach involves some misconfiguration. Defaults that are insecure are a systemic failure.

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **Environment-aware config** | Production = strict, development = relaxed, explicit toggle |
| **Security headers by default** | Helmet-like middleware that sets 15+ headers automatically |
| **Error handling** | Generic errors in production, verbose in development |
| **Feature flags** | Disable unused features at config level, not code level |
| **Config validation** | Fail fast on insecure configurations in production |

**What GoFastr should do:**
- `gofastr.New()` applies production-secure defaults
- `gofastr.New(gofastr.DevMode())` relaxes for local development with warnings
- Auto-set security headers on every response
- Redact sensitive values from config dumps and logs
- Fail to start in production with insecure config (HTTP mode, debug mode, default secrets)

```go
// Production — strict by default
app := gofastr.New()

// Development — relaxed, with console warnings
app := gofastr.New(gofastr.DevMode())

// Impossible to accidentally run production with dev settings
// gofastr will refuse to start if GO_ENV=production and DevMode() is set
```

---

### 1.6 Vulnerable & Outdated Components

**The threat:** Using libraries with known vulnerabilities. The log4j incident (CVE-2021-44228) affected millions of systems.

**Real-world impact:** Supply chain attacks are the fastest-growing attack vector. A single vulnerable dependency can compromise your entire application.

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **Dependency auditing** | `govulncheck`, `npm audit`, `bundle audit` |
| **Lock files** | Reproducible builds, prevent silent dependency updates |
| **SBOM generation** | Software Bill of Materials for transparency |
| **Minimal dependencies** | Fewer deps = smaller attack surface |
| **Automated updates** | Dependabot, Renovate for continuous patching |
| **Vulnerability databases** | Go Vulnerability Database, Snyk, OSV |

**What GoFastr should do:**
- Minimize external dependencies — prefer Go stdlib
- Ship `govulncheck` integration in the CLI
- Provide `gofastr deps audit` that checks all transitive dependencies
- Document every dependency with justification
- Pin all dependency versions in go.sum
- Include SBOM generation in build pipeline

---

### 1.7 Authentication & Identification Failures

**The threat:** Weak passwords, credential stuffing, session fixation, missing MFA, session IDs in URLs, long-lived sessions, cleartext password storage.

**Real-world impact:** Account takeover, identity theft, lateral movement within the application.

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **Password hashing** | bcrypt/argon2 with work factor auto-tuning |
| **Session management** | Secure, HttpOnly, SameSite cookies; server-side session stores |
| **MFA support** | TOTP, WebAuthn/FIDO2, backup codes |
| **Credential rotation** | Force password change after breach detection |
| **Brute force protection** | Rate limiting on auth endpoints, account lockout |
| **Session invalidation** | On password change, logout all devices, session expiry |

**What GoFastr should do:**
- Built-in session management with secure cookie store
- Pluggable auth backends (session-based, JWT, OAuth2/OIDC)
- Built-in TOTP and WebAuthn support
- Rate limiting on login, registration, and password reset routes by default
- Automatic session rotation after privilege escalation
- Breached password detection via HaveIBeenPwned API (k-anonymity model)

---

### 1.8 Software & Data Integrity Failures

**The threat:** Code or data that's been tampered with — insecure CI/CD pipelines, unsigned updates, deserialization of untrusted data, CDNs serving malicious JS.

**Real-world impact:** The SolarWinds attack (2020) compromised 18,000+ organizations through a poisoned software update.

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **Subresource Integrity (SRI)** | Hash-verified external scripts and stylesheets |
| **Signed builds** | Verify artifact integrity from CI to production |
| **Safe deserialization** | Type-safe data binding; never deserialize arbitrary structures |
| **Dependency pinning** | Exact versions + integrity hashes |
| **CI/CD security** | Signed commits, protected branches, minimal build permissions |

**What GoFastr should do:**
- Auto-generate SRI hashes for all static assets
- Use Go's `encoding/json` with strict struct binding (no `map[string]interface{}` for user input)
- Provide build signing and verification tools
- Verify dependency checksums at build time
- Reject unsigned or tampered assets at runtime

---

### 1.9 Security Logging & Monitoring Failures

**The threat:** No logging, insufficient logging, logged sensitive data, no alerting, logs that attackers can tamper with.

**Real-world impact:** Without logging, breaches go undetected for months (average: 287 days per IBM). Without monitoring, response is reactive, not proactive.

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **Structured logging** | JSON logs with consistent fields for machine parsing |
| **Security event logging** | Auth failures, access denials, input validation failures, rate limit hits |
| **Audit trails** | Who did what, when, from where — tamper-evident |
| **PII redaction** | Auto-strip passwords, tokens, SSNs from log output |
| **Alert integration** | Webhook/SIEM hooks for anomaly detection |
| **Request tracing** | Correlation IDs across services |

**What GoFastr should do:**
- Built-in structured logger (slog) with security event categories
- Auto-log: auth events, access denials, rate limit triggers, input validation failures, errors
- Auto-redact: passwords, tokens, cookies, API keys from all log output
- Request ID middleware for distributed tracing
- Integration hooks for SIEM systems (Datadog, Splunk, etc.)
- Tamper-evident audit log storage option

```go
// Auto-logged security events
app.Use(gofastr.SecurityLogger(
    gofastr.LogAuthEvents(),        // login, logout, failed attempts
    gofastr.LogAccessDenials(),     // 401, 403 responses
    gofastr.LogRateLimitHits(),     // throttled requests
    gofastr.LogInputViolations(),   // validation failures
    gofastr.RedactFields("password", "token", "ssn", "credit_card"),
))
```

---

### 1.10 Server-Side Request Forgery (SSRF)

**The threat:** The application makes HTTP requests to user-supplied URLs, allowing attackers to reach internal services, cloud metadata endpoints (169.254.169.254), and internal APIs.

**Real-world impact:** The Capital One breach (2019) exploited SSRF to access AWS metadata and steal 100M+ records.

**How frameworks mitigate it:**

| Mechanism | Description |
|-----------|-------------|
| **URL allowlisting** | Only allow requests to pre-approved domains/IPs |
| **Block internal IPs** | Deny RFC 1918, loopback, link-local addresses |
| **No raw URL passthrough** | Validate and normalize URLs before fetching |
| **DNS rebinding protection** | Resolve DNS before connecting, verify resolved IP isn't internal |
| **Network policies** | Egress filtering at the infrastructure level |

**What GoFastr should do:**
- Provide a safe HTTP client that blocks internal IPs by default
- URL validation helper: parse, normalize, check against allowlist
- Block cloud metadata endpoints (169.254.169.254, metadata.google.internal)
- Optional: require explicit opt-in for outbound HTTP requests in handlers
- Log all outbound requests for audit trails

```go
// Safe HTTP client — blocks internal/metadata IPs by default
safeClient := gofastr.SafeHTTPClient(
    gofastr.BlockInternalIPs(),
    gofastr.BlockMetadataEndpoints(),
    gofastr.AllowDomains("api.stripe.com", "api.github.com"),
    gofastr.MaxRedirects(0), // prevent redirect-based bypass
)
```

---

## 2. Framework-Level Security Features

### 2.1 CSRF Protection (Cross-Site Request Forgery)

**The problem:** A malicious site tricks the user's browser into making authenticated requests to your app. The browser includes cookies automatically.

#### Mechanisms

| Strategy | How It Works | Pros | Cons |
|----------|-------------|------|------|
| **Synchronizer Token** | Server generates a secret token per session; form includes it as a hidden field; server verifies on POST | Battle-tested, works everywhere | Requires token in every form, tricky for AJAX |
| **Double-Submit Cookie** | Set a cookie with a random value; require JS to read the cookie and include it as a header; server verifies cookie == header | Stateless, easy for SPAs | Vulnerable to subdomain attacks without Subresource Integrity |
| **SameSite Cookie** | `SameSite=Strict` or `SameSite=Lax` prevents browser from sending cookies on cross-site requests | Zero server changes needed | Older browser support, `Lax` allows top-level GET redirects |
| **Origin/Referer Header** | Verify the `Origin` or `Referer` header matches your domain | Simple | Missing on some older browsers/privacy tools |

**Best practice: Defense in depth**
1. Set `SameSite=Lax` on all session cookies (strict breaks external link follows)
2. Generate a CSRF token per session (synchronizer token pattern)
3. Require the token on all state-changing requests (POST, PUT, PATCH, DELETE)
4. Validate `Origin` header as a secondary check

**GoFastr recommendation:**
```go
app.Use(gofastr.CSRF(
    gofastr.CSRFTokenMethod(gofastr.SynchronizerToken), // primary
    gofastr.SameSiteMode(http.SameSiteLaxMode),          // secondary
    gofastr.OriginValidation(true),                       // tertiary
    gofastr.CookieName("csrf_token"),
    gofastr.FieldName("_csrf"),
    gofastr.HeaderName("X-CSRF-Token"),
))
```

---

### 2.2 XSS Prevention (Cross-Site Scripting)

**The problem:** Untrusted data is rendered in the browser without escaping, allowing attackers to inject `<script>` tags or JavaScript URLs.

#### Prevention Layers

| Layer | Mechanism | Description |
|-------|-----------|-------------|
| **Template auto-escaping** | `html/template` in Go | Escapes `{{.Data}}` by default; must explicitly use `{{.Data \| html}}` for raw output |
| **Content Security Policy** | CSP header | Restricts script sources, prevents inline scripts, limits `eval()` |
| **HTTP-only cookies** | Cookie flag | JavaScript can't read session cookies |
| **Input sanitization** | Server-side | Strip or encode dangerous HTML before storage/rendering |
| **Output encoding** | Context-aware | Different encoding for HTML body, attributes, JavaScript, URLs, CSS |
| **Trusted Types** | Browser API | Enforces compile-time safety for DOM sinks |

**GoFastr recommendation:**
- Use `html/template` exclusively for HTML rendering
- Set CSP header that disallows `unsafe-inline` and `unsafe-eval`
- Provide a `SanitizeHTML()` helper that uses a allowlist-based sanitizer
- Set `X-Content-Type-Options: nosniff` to prevent MIME sniffing
- Auto-set `HttpOnly` and `Secure` on all cookies

```go
// CSP configuration — strict by default
app.Use(gofastr.ContentSecurityPolicy(
    gofastr.DefaultSrc(gofastr.Self),
    gofastr.ScriptSrc(gofastr.Self, gofastr.Nonce),       // nonce-based, no inline
    gofastr.StyleSrc(gofastr.Self, gofastr.Nonce),
    gofastr.ImgSrc(gofastr.Self, "data:"),
    gofastr.ConnectSrc(gofastr.Self),
    gofastr.FrameAncestors(gofastr.None),                  // prevent clickjacking
    gofastr.BaseURI(gofastr.Self),
    gofastr.FormAction(gofastr.Self),
    gofastr.ReportURI("/csp-reports"),
))
```

---

### 2.3 SQL Injection Prevention

**The problem:** User input is concatenated into SQL queries, allowing attackers to modify the query's logic.

#### Prevention Strategies (ranked by effectiveness)

1. **Parameterized queries** — Always use `$1`, `$2` placeholders. Never concatenate.
2. **Query builders** — Type-safe API that generates parameterized SQL.
3. **ORM** — Object-relational mapping that handles query construction.
4. **Input validation** — Allowlist validation before data reaches the query layer.
5. **Least-privilege DB users** — Application DB user can't `DROP TABLE`.

**GoFastr recommendation:**
- Ship a query builder that makes string concatenation unnatural
- Provide first-class support for `database/sql` parameterized queries
- Include a `Raw()` escape hatch with mandatory safety warning in docs
- Validate input with schema validation before it reaches the database layer
- Include a lint rule that flags string concatenation in query contexts

```go
// Type-safe query builder — injection impossible by design
users, err := db.Query(ctx,
    query.Select("id", "email", "name").
        From("users").
        Where(query.Eq("email", userEmail)).
        Where(query.Eq("status", "active")).
        Limit(1),
)
```

---

### 2.4 Authentication Patterns

#### Session-Based Authentication (Recommended Default)

| Aspect | Detail |
|--------|--------|
| **How it works** | Server creates a session, stores it (memory/Redis/DB), sends session ID in a cookie |
| **Pros** | Server can invalidate instantly; no token management complexity; built into browsers |
| **Cons** | Requires server-side state; not ideal for pure APIs |
| **Security** | Secure, HttpOnly, SameSite cookies; session rotation; absolute timeout |

#### JWT (JSON Web Tokens)

| Aspect | Detail |
|--------|--------|
| **How it works** | Server issues signed (or encrypted) token; client stores and sends it |
| **Pros** | Stateless; works across services; no server-side storage |
| **Cons** | Cannot easily revoke; token in local storage = XSS risk; size grows with claims |
| **Security** | Short-lived access tokens + refresh tokens; RS256/ES256 signing; validate `iss`, `aud`, `exp` |

**⚠️ JWT anti-patterns to avoid:**
- Storing JWTs in `localStorage` (XSS-vulnerable)
- Long-lived access tokens (use 5-15 minutes max)
- Including sensitive data in the payload (it's base64, not encrypted)
- Using `none` algorithm (always validate `alg` header)
- Not rotating signing keys

#### OAuth2 / OpenID Connect

| Aspect | Detail |
|--------|--------|
| **How it works** | Delegate authentication to an identity provider (Google, GitHub, Okta) |
| **Pros** | Offloads password management; users prefer social login; MFA handled by provider |
| **Cons** | Dependency on external service; complex flows; token management |
| **Security** | Use PKCE for public clients; validate `state` parameter; store tokens securely |

#### Passkeys / WebAuthn (FIDO2)

| Aspect | Detail |
|--------|--------|
| **How it works** | Public-key cryptography; device authenticates with biometric/PIN |
| **Pros** | Phishing-resistant; no passwords; device-bound credentials |
| **Cons** | Requires device support; complex implementation; account recovery is different |
| **Security** | Gold standard for authentication; eliminates credential theft |

**GoFastr recommendation:**
```go
// Built-in auth with multiple strategies
auth := gofastr.Auth(
    gofastr.SessionAuth(                          // default for web apps
        gofastr.SessionStore(redisStore),
        gofastr.SessionTimeout(12*time.Hour),
        gofastr.AbsoluteTimeout(7*24*time.Hour),
    ),
    gofastr.JWTAuth(                              // opt-in for APIs
        gofastr.AccessTokenDuration(15*time.Minute),
        gofastr.RefreshTokenDuration(7*24*time.Hour),
        gofastr.SigningMethod(gofastr.ES256),
    ),
    gofastr.OAuthProvider("google", googleConfig),
    gofastr.OAuthProvider("github", githubConfig),
    gofastr.WebAuthn(webauthnConfig),             // passkeys
    gofastr.RequireMFA(gofastr.TOTP),             // optional MFA
)
```

---

### 2.5 Authorization Patterns

#### RBAC (Role-Based Access Control)

```
User → has Role → has Permissions
```
Simple, widely understood. Works for 80% of cases.

```go
router.GET("/admin/users",
    middleware.RequireRole("admin"),
    handlers.ListUsers,
)
```

#### ABAC (Attribute-Based Access Control)

```
User + Resource + Environment → Decision
```
Context-aware. Example: "Users can edit their own posts OR users with role=editor can edit posts in their department."

```go
router.PATCH("/posts/:id",
    middleware.RequireAuth(),
    middleware.RequirePolicy("posts.edit", gofastr.Policy{
        "owner":   true,       // author can edit own posts
        "role":    "editor",   // editors can edit in their department
        "status":  "draft",    // only draft posts can be edited
    }),
    handlers.UpdatePost,
)
```

#### Policy-Based (OPA/Cedar/Casbin)

External policy engine. Best for complex, multi-service environments.

```go
// Example with embedded policy engine
policy := gofastr.PolicyEngine(casbin.NewEnforcer("model.conf", "policy.csv"))
router.Use(gofastr.PolicyMiddleware(policy))
```

#### Middleware Guards (Route-Level)

The most common pattern in web frameworks:

```go
// Chain authorization checks as middleware
router.GET("/api/admin/stats",
    middleware.RequireAuth(),        // must be logged in
    middleware.RequireRole("admin"), // must have admin role
    middleware.RequireScope("read:stats"), // must have specific scope
    handlers.GetAdminStats,
)
```

**GoFastr recommendation:**
- Built-in RBAC with simple role/permission model
- ABAC via policy functions for complex cases
- Middleware-based route guards as the primary API
- Resource-level authorization helpers (scope to current user)
- Policy engine integration via interface

---

### 2.6 Rate Limiting

#### Algorithms

| Algorithm | How It Works | Best For |
|-----------|-------------|----------|
| **Fixed Window** | Count requests in fixed time windows (e.g., per minute) | Simple, predictable |
| **Sliding Window Log** | Track each request timestamp, count within sliding window | Accurate, memory-heavy |
| **Sliding Window Counter** | Hybrid: weighted average of current and previous window | Good balance |
| **Token Bucket** | Tokens replenish at fixed rate; each request costs 1 token | Burst-friendly |
| **Leaky Bucket** | Requests processed at fixed rate; excess queued or rejected | Smooth traffic |

#### Per-Route Configuration

```go
rateLimiter := gofastr.RateLimiter(
    gofastr.Store(redisStore),  // distributed rate limiting
    gofastr.DefaultLimit(100, time.Minute), // 100 req/min by default
)

// Per-route overrides
router.POST("/auth/login",
    rateLimiter.Limit(5, time.Minute),     // strict: 5 attempts/min
    rateLimiter.Limit(20, time.Hour),      // 20 attempts/hour
    handlers.Login,
)

router.POST("/api/data",
    rateLimiter.Limit(1000, time.Minute),  // relaxed for API
    handlers.GetData,
)

// Key-based limiting (per user, per IP, per API key)
rateLimiter.KeyBy(gofastr.IPKey)           // default: IP address
rateLimiter.KeyBy(gofastr.UserKey)         // per authenticated user
rateLimiter.KeyBy(gofastr.CustomKey(func(r *http.Request) string {
    return r.Header.Get("X-API-Key")
}))
```

**GoFastr recommendation:**
- Token bucket algorithm (good for APIs, allows reasonable bursts)
- Sliding window for auth endpoints (strict, accurate)
- Per-IP by default, per-user when authenticated
- Built-in backoff headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
- Automatic 429 responses with `Retry-After` header
- Redis-backed for distributed rate limiting

---

### 2.7 Input Validation & Sanitization

#### Schema Validation

```go
type RegistrationInput struct {
    Email    string `validate:"required,email,max=255"`
    Password string `validate:"required,min=12,max=128"`
    Name     string `validate:"required,min=1,max=100"`
    Age      int    `validate:"required,min=13,max=150"`
}

// Automatic validation on request binding
var input RegistrationInput
if err := gofastr.BindAndValidate(r, &input); err != nil {
    return gofastr.ValidationError(err)
}
```

#### Validation Rules

| Category | Rules |
|----------|-------|
| **String** | required, min, max, len, email, url, uuid, alpha, alphanum, regex |
| **Numeric** | required, min, max, gt, lt, positive, negative |
| **Collections** | min_len, max_len, contains, unique |
| **Special** | datetime, ip, cidr, semver, json, base64, phone |
| **Custom** | User-defined validator functions |

#### Sanitization

```go
// Automatic sanitization during binding
type CommentInput struct {
    Body    string `sanitize:"trim,stripTags,max=5000"`
    Email   string `sanitize:"trim,normalizeEmail"`
    Website string `sanitize:"trim,escapeURL"`
}
```

#### Type Coercion

Go's static typing provides compile-time safety. Framework should:
- Enforce strict type checking at the binding layer
- Return clear errors for type mismatches
- Never silently coerce to unexpected types

**GoFastr recommendation:**
- Struct-tag-based validation (familiar to Go developers)
- Composable validators: `validate:"required,email"`
- Custom validator registration
- Automatic sanitization pipeline
- Type-safe binding — compile-time guarantees where possible

---

### 2.8 Secure Headers

The complete set of security headers a framework should set by default:

```http
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 0                          # Deprecated, but set to 0 to disable buggy filter
Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Resource-Policy: same-origin
Cross-Origin-Embedder-Policy: require-corp    # Optional, can break third-party resources
```

#### Header Explanations

| Header | Purpose |
|--------|---------|
| **Content-Security-Policy** | Prevents XSS by controlling which resources can load |
| **X-Content-Type-Options** | Prevents MIME type sniffing |
| **X-Frame-Options** | Prevents clickjacking (legacy, superseded by CSP `frame-ancestors`) |
| **Strict-Transport-Security** | Forces HTTPS for future visits |
| **Referrer-Policy** | Controls how much referrer info is shared |
| **Permissions-Policy** | Controls which browser features can be used |
| **COOP/COEP/CORP** | Prevents Spectre-based cross-origin leaks |

**GoFastr recommendation:**
```go
// One-line secure headers — applies all recommended headers
app.Use(gofastr.SecureHeaders())

// Customize specific headers
app.Use(gofastr.SecureHeaders(
    gofastr.HSTSMaxAge(365*24*time.Hour),
    gofastr.HSTSIncludeSubDomains(true),
    gofastr.HSTSPreload(true),
    gofastr.FrameOptions(gofastr.FrameDeny),
    gofastr.CSP(myCSPDirective),
    gofastr.ReferrerPolicy(gofastr.StrictOriginWhenCrossOrigin),
))
```

---

### 2.9 Session Management

#### Secure Cookie Configuration

```go
session := gofastr.SessionConfig{
    CookieName:     "session",
    MaxAge:         12 * time.Hour,          // idle timeout
    AbsoluteMaxAge: 7 * 24 * time.Hour,      // absolute timeout regardless of activity
    Secure:         true,                      // HTTPS only
    HttpOnly:       true,                      // no JS access
    SameSite:       http.SameSiteLaxMode,     // CSRF protection
    Domain:         "example.com",
    Path:           "/",
}
```

#### Session Rotation

- **On authentication** — new session ID after login
- **On privilege change** — new session ID when user gains/loses roles
- **On password change** — invalidate all other sessions
- **Periodic** — rotate session ID every N requests or minutes
- **On suspicious activity** — force re-authentication

#### Session Storage Backends

| Backend | Pros | Cons |
|---------|------|------|
| **In-memory** | Fastest, simplest | Lost on restart; doesn't scale across instances |
| **Redis** | Fast, distributed, supports TTL | External dependency |
| **Database** | Durable, queryable | Slower, more load on DB |
| **Encrypted cookie** | No server state | Size limited (~4KB); harder to invalidate |

**GoFastr recommendation:**
- Encrypted cookie sessions for simplicity (single-server)
- Redis session store for production (distributed, scalable)
- Built-in session rotation on all privilege changes
- Configurable idle and absolute timeouts
- "Logout everywhere" support
- Flash messages via session (success/error messages that clear after display)

---

### 2.10 Secret Management

#### Hierarchy of Secret Management

1. **Environment variables** — Baseline. Works everywhere. Never commit to git.
2. **`.env` files** — Development only. Add to `.gitignore`. Auto-loaded in dev mode.
3. **Secret manager services** — AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault
4. **Encrypted config files** — SOPS, age, or framework-native encryption
5. **Runtime injection** — Kubernetes secrets, Docker secrets, systemd credentials

#### Rules for Secret Handling

- **Never log secrets** — Auto-redact from all log output
- **Never expose in error messages** — Sanitize stack traces
- **Never store in source control** — Use `.env.example` with placeholder values
- **Rotate regularly** — Automated rotation for service accounts
- **Encrypt at rest** — Database credentials, API keys, signing keys
- **Minimal access** — Each service only gets the secrets it needs

**GoFastr recommendation:**
```go
// Load secrets from multiple sources with priority
secrets := gofastr.Secrets(
    gofastr.FromEnv(),                           // GOFASTR_DB_PASSWORD, etc.
    gofastr.FromDotEnv(),                        // .env file (dev only, warned in production)
    gofastr.FromVault(vaultConfig),              // HashiCorp Vault
    gofastr.Required("DATABASE_URL", "SECRET_KEY"),
)

// Automatic redaction from logs and error responses
app.Use(gofastr.SecretRedaction(
    "password", "token", "secret", "api_key",
    "credit_card", "ssn", "authorization",
))

// Startup validation — fail if required secrets are missing
// app.Start() will panic with a clear message if required secrets aren't set
```

---

### 2.11 CORS Configuration

#### What CORS Protects Against

CORS (Cross-Origin Resource Sharing) controls which origins can make requests to your API from a browser. It does NOT protect against server-to-server requests.

#### Configuration

```go
app.Use(gofastr.CORS(gofastr.CORSConfig{
    AllowOrigins:     []string{"https://example.com", "https://app.example.com"},
    AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
    AllowHeaders:     []string{"Accept", "Content-Type", "X-CSRF-Token", "Authorization"},
    ExposeHeaders:    []string{"X-Request-ID"},
    AllowCredentials: true,
    MaxAge:           12 * time.Hour,    // preflight cache
}))
```

#### Common Misconfigurations

| Misconfiguration | Risk | Correct Approach |
|------------------|------|-----------------|
| `AllowOrigin: *` | Any website can call your API | Explicit origin allowlist |
| `AllowOrigin: request.Origin` | Reflects any origin (disables CORS protection) | Validate against allowlist |
| `AllowCredentials: true` + `*` | Invalid by spec, but some implementations mishandle it | Explicit origins when using credentials |
| Missing `Vary: Origin` | CDN/proxy caching serves wrong CORS headers | Always set `Vary: Origin` |
| Over-permissive methods | Exposes DELETE, PUT to any origin | Only allow what's needed |

**GoFastr recommendation:**
- Strict origin allowlist by default
- `AllowCredentials: false` by default
- Dynamic origin validation function for multi-tenant apps
- Automatic `Vary: Origin` header
- Preflight caching via `MaxAge`
- Log blocked CORS requests for debugging

---

### 2.12 Dependency Auditing & Supply Chain Security

#### Multi-Layer Defense

| Layer | Tool | Frequency |
|-------|------|-----------|
| **Known vulnerability scanning** | `govulncheck`, Snyk, Trivy | Every build |
| **License compliance** | `go-licenses`, FOSSA | Every PR |
| **Dependency pinning** | `go.sum` verification | Every build |
| **SBOM generation** | `syft`, CycloneDX | Every release |
| **Integrity verification** | `go mod verify` | Every build |
| **Renovate/Dependabot** | Automated PRs for updates | Continuous |

**GoFastr recommendation:**
- `gofastr audit` CLI command that runs all checks
- `gofastr sbom` generates Software Bill of Materials
- `gofastr deps update` safely updates dependencies with test verification
- Pre-commit hook for `govulncheck`
- CI pipeline template with all checks built-in

---

### 2.13 Content Security Policy (CSP) Generation

CSP is the most important XSS prevention header but notoriously complex to configure correctly.

#### Nonce-Based CSP (Recommended)

```go
// Each request gets a unique nonce
app.Use(gofastr.CSPNonce()) // injects nonce into templates

// In templates:
// <script nonce="{{.CSPNonce}}">...</script>

// CSP header auto-includes the nonce
// Content-Security-Policy: script-src 'nonce-abc123'; style-src 'nonce-abc123'
```

#### CSP Reporting

```go
// Report-only mode — monitor violations without breaking things
app.Use(gofastr.CSPReportOnly(gofastr.CSPConfig{
    ReportURI: "/csp-reports",
}))

// Handle reports
router.POST("/csp-reports", handlers.CSPReportHandler)
```

#### CSP Levels

| Level | Use Case | Policy |
|-------|----------|--------|
| **Strict** | New apps, no legacy constraints | No inline scripts, no eval, nonce-based |
| **Moderate** | Apps with some inline scripts | Nonce + specific inline hashes |
| **Report-only** | Migrating existing apps | Monitor violations, don't enforce |

---

### 2.14 Subresource Integrity (SRI)

```html
<!-- Without SRI: CDN compromise can inject malicious JS -->
<script src="https://cdn.example.com/lib.js"></script>

<!-- With SRI: browser verifies hash before executing -->
<script
  src="https://cdn.example.com/lib.js"
  integrity="sha384-oqVuAfXRKap7fdgcCY5uykM6+R9GqQ8K/uxy9rx7HNQlGYl1kPzQho1wx4JwY8wC"
  crossorigin="anonymous"
></script>
```

**GoFastr recommendation:**
- Auto-generate SRI hashes during asset compilation
- Provide template helpers: `{{SRI "js/app.js"}}` → generates full tag with integrity
- Warn if external resources are loaded without SRI
- Include SRI in the `gofastr audit` command

---

### 2.15 HTTPS Enforcement & TLS Configuration

#### TLS Best Practices (2025)

| Setting | Recommended Value |
|---------|-------------------|
| **Minimum TLS version** | TLS 1.2 (prefer TLS 1.3) |
| **Cipher suites** | AES-256-GCM, ChaCha20-Poly1305 |
| **Certificate type** | ECDSA P-256 or RSA 2048+ |
| **Certificate source** | Let's Encrypt (ACME), managed certs |
| **HSTS** | `max-age=63072000; includeSubDomains; preload` |
| **OCSP stapling** | Enabled |
| **HTTP→HTTPS redirect** | 301 permanent redirect |

```go
app := gofastr.New(
    gofastr.TLS(gofastr.TLSConfig{
        MinVersion:   tls.VersionTLS12,
        PreferTLS13:  true,
        AutoCert:     gofastr.LetsEncrypt("certs/", "admin@example.com"),
        HTTPRedirect: true,    // redirect all HTTP to HTTPS
        HSTS:         true,    // set Strict-Transport-Security header
        HSTSPreload:  true,
    }),
)

// Development: skip TLS
app := gofastr.New(gofastr.DevMode()) // HTTP only, warned on startup
```

---

## 3. How Modern Frameworks Handle Security

### 3.1 Next.js

**Philosophy:** Security by default for the React ecosystem, with escape hatches for advanced users.

| Feature | Implementation |
|---------|---------------|
| **CSRF** | Relies on SameSite cookies; no built-in token system |
| **XSS** | JSX auto-escapes; React avoids `dangerouslySetInnerHTML` by design |
| **Headers** | `next.config.js` headers section; no built-in Helmet equivalent |
| **CSP** | Nonce-based CSP via middleware; requires manual configuration |
| **Auth** | NextAuth.js / Auth.js — community solution, not built-in |
| **ISR/SSG** | Static generation reduces attack surface |
| **API routes** | Built-in body parsing with size limits |
| **Image optimization** | Prevents malicious image exploitation via server-side processing |

**Strengths:** JSX auto-escaping, Server Components reduce client-side attack surface
**Weaknesses:** Security headers require manual config; no built-in auth; CSP is opt-in

---

### 3.2 Ruby on Rails

**Philosophy:** "Secure by convention." Rails has the strongest security-by-default story of any framework.

| Feature | Implementation |
|---------|---------------|
| **CSRF** | `protect_from_forgery` enabled by default in `ApplicationController`; per-form tokens |
| **XSS** | ERB auto-escapes all output since Rails 3; `html_safe` required for raw HTML |
| **SQL injection** | ActiveRecord parameterizes all queries; raw SQL requires explicit `sanitize_sql` |
| **Strong params** | `require` and `permit` whitelist parameters; mass assignment impossible by default |
| **Session** | Encrypted cookies by default; configurable store |
| **Headers** | Security headers via `config.force_ssl`, `config.action_dispatch.default_headers` |
| **Auth** | Devise gem (community standard); has_secure_password in ActiveModel |
| **Rate limiting** | `ActionController::Throttling` (Rails 8+); previously via Rack::Attack gem |
| **Content Security** | CSP configuration in `config.content_security_policy` |
| **Credential management** | `config/credentials.yml.enc` — encrypted at rest |

**The Rails Way:**
```ruby
# Strong parameters — impossible to mass-assign without permission
def user_params
  params.require(:user).permit(:name, :email)  # only these fields allowed
end

# CSRF protection — enabled by default, no code needed
class ApplicationController < ActionController::Base
  protect_from_forgery with: :exception  # raises on invalid CSRF token
end
```

**Key lesson for GoFastr:** Rails proves that security defaults work at scale. Developers rarely disable them.

---

### 3.3 Laravel

**Philosophy:** Expressive security APIs that make the secure path the easy path.

| Feature | Implementation |
|---------|---------------|
| **CSRF** | `@csrf` Blade directive in every form; `VerifyCsrfToken` middleware on by default |
| **XSS** | Blade `{{ }}` auto-escapes; `{!! !!}` for raw output (explicitly dangerous) |
| **SQL injection** | Eloquent ORM + query builder parameterize by default |
| **Auth** | First-party `laravel/breeze`, `laravel/jetstream`, `laravel/fortify` |
| **Authorization** | Gates and Policies — expressive, testable |
| **Encryption** | `Crypt` facade using AES-256-CBC; auto-generated APP_KEY |
| **Hashing** | `Hash` facade using bcrypt/argon2; auto-hash on model save |
| **Rate limiting** | `RateLimiter` facade with per-route configuration |
| **Sanitization** | Middleware for input trimming and HTML stripping |
| **Headers** | `SetCacheHeaders`, custom middleware for security headers |

**Key lesson for GoFastr:** Laravel's gates and policies pattern is excellent for expressive authorization without boilerplate.

---

### 3.4 Django

**Philosophy:** "Batteries included" security — comprehensive middleware stack activated by default.

| Feature | Implementation |
|---------|---------------|
| **CSRF** | `CsrfViewMiddleware` enabled by default; `{% csrf_token %}` in templates |
| **XSS** | Django templates auto-escape by default; `|safe` filter for raw output |
| **SQL injection** | Django ORM parameterizes all queries; raw SQL requires `RawSQL` or `extra()` |
| **Clickjacking** | `XFrameOptionsMiddleware` sets `X-Frame-Options: DENY` by default |
| **SSL/HTTPS** | `SecurityMiddleware` handles SSL redirect, HSTS, secure cookies |
| **Auth** | Built-in `django.contrib.auth` — users, groups, permissions |
| **Session** | Signed cookies or database-backed; secure settings in `SESSION_COOKIE_*` |
| **Headers** | `SecurityMiddleware` sets comprehensive security headers |
| **Password validation** | Built-in validators: minimum length, common passwords, similarity to user attributes |
| **Content Security** | `django-csp` package; `SECURE_CONTENT_TYPE_NOSNIFF` setting |

**Django Security Middleware Stack (settings.py):**
```python
MIDDLEWARE = [
    'django.middleware.security.SecurityMiddleware',      # HTTPS, HSTS, secure refs
    'django.contrib.sessions.middleware.SessionMiddleware',
    'django.middleware.csrf.CsrfViewMiddleware',          # CSRF protection
    'django.middleware.clickjacking.XFrameOptionsMiddleware', # Clickjacking
    'django.middleware.common.CommonMiddleware',           # URL normalization
]

# Security settings
SECURE_SSL_REDIRECT = True
SECURE_HSTS_SECONDS = 63072000
SECURE_HSTS_INCLUDE_SUBDOMAINS = True
SECURE_HSTS_PRELOAD = True
SESSION_COOKIE_SECURE = True
CSRF_COOKIE_SECURE = True
SECURE_BROWSER_XSS_FILTER = True
SECURE_CONTENT_TYPE_NOSNIFF = True
```

**Key lesson for GoFastr:** Django's security middleware stack should be the model — ordered, comprehensive, on by default.

---

### 3.5 Phoenix (Elixir)

**Philosophy:** Security through the BEAM VM's isolation model and Plug middleware pipeline.

| Feature | Implementation |
|---------|---------------|
| **CSRF** | `protect_from_forgery` plug in browser pipeline by default |
| **XSS** | Phoenix templates (HEEx) auto-escape; `raw()` for unescaped output |
| **SQL injection** | Ecto query builder parameterizes by default |
| **Auth** | `phx.gen.auth` — built-in authentication generator (since 1.6) |
| **Session** | Cookie-based sessions with configurable store |
| **Rate limiting** | Hammer or ExRated libraries; not built-in |
| **Headers** | Custom plugs for security headers |
| **LiveView** | Server-rendered, reduced client-side attack surface |
| **Channels** | Token-based WebSocket authentication |

**Key lesson for GoFastr:** Phoenix's `phx.gen.auth` approach — generate a complete, secure auth system — is excellent for developer experience.

---

## 4. Security-First Architecture for a Go Framework

### 4.1 What Should Be ON by Default

These features are enabled when you call `gofastr.New()` with no options:

| Feature | Default Behavior | Rationale |
|---------|-----------------|-----------|
| **HTTPS redirect** | Redirect HTTP→HTTPS in production | Prevents cleartext transmission |
| **HSTS** | `max-age=63072000; includeSubDomains` | Forces HTTPS for repeat visits |
| **Secure cookies** | `Secure; HttpOnly; SameSite=Lax` | Prevents XSS, CSRF, MITM cookie theft |
| **CSRF protection** | Synchronizer token + SameSite | State-changing requests require token |
| **XSS auto-escaping** | `html/template` for all HTML output | Prevents script injection |
| **Security headers** | Full Helmet-like header set | Defense in depth |
| **Input validation** | Struct-tag validation on all bound input | Prevents injection, bad data |
| **Session management** | Encrypted cookie sessions with rotation | Prevents session fixation/hijacking |
| **Error hiding** | Generic errors in production, verbose in dev | Prevents info leakage |
| **Request ID** | UUID per request in header and logs | Tracing and debugging |
| **CORS** | Deny all cross-origin requests | Explicit allowlist required |
| **Rate limiting** | Basic per-IP rate limiting on auth routes | Prevents brute force |
| **Security logging** | Auth events, access denials, input violations | Breach detection |
| **PII redaction** | Auto-redact sensitive fields from logs | Privacy compliance |

### What Should Be Opt-In

| Feature | Why Opt-In | How to Enable |
|---------|-----------|---------------|
| **JWT authentication** | Session auth is simpler and more secure for most web apps | `gofastr.JWTAuth(...)` |
| **OAuth2 providers** | Requires external configuration | `gofastr.OAuthProvider(...)` |
| **WebAuthn/Passkeys** | Requires additional setup | `gofastr.WebAuthn(...)` |
| **Redis sessions** | Requires external service | `gofastr.SessionStore(redis)` |
| **Strict CSP** | Can break third-party integrations | `gofastr.ContentSecurityPolicy(...)` |
| **CSP report-only** | Monitoring mode | `gofastr.CSPReportOnly(...)` |
| **MFA** | Requires user enrollment flow | `gofastr.RequireMFA(...)` |
| **Vault integration** | Requires external service | `gofastr.FromVault(...)` |
| **ABAC policies** | Complexity not needed for simple apps | `gofastr.PolicyEngine(...)` |
| **SSRF protection** | Only needed for outbound HTTP features | `gofastr.SafeHTTPClient(...)` |

### 4.2 Making Insecure Paths Hard and Secure Paths Easy

**Principle:** The default should always be the secure option. Insecure options should require:
1. Explicit opt-in
2. A clear name (e.g., `gofastr.InsecureDevMode()`, `gofastr.UnsafeRawQuery()`)
3. A visible warning in logs
4. A compile-time or startup-time error in production

**Examples:**

```go
// ✅ Easy and secure (default)
app.GET("/users/:id", handler)

// ❌ Hard — requires explicit "Unsafe" prefix
results, err := db.UnsafeRawQuery("SELECT * FROM " + table)

// ❌ Hard — fails in production, warns in dev
app := gofastr.New(gofastr.InsecureDevMode())

// ✅ Easy — built-in safe alternatives
app.Use(gofastr.CSRF())                           // one line
app.Use(gofastr.SecureHeaders())                   // one line
app.Use(gofastr.RateLimiter(gofastr.Default()))    // one line
```

**Naming conventions for unsafe operations:**
- `Unsafe*` prefix for escape hatches (e.g., `UnsafeRawHTML()`)
- `Insecure*` prefix for disabled security (e.g., `InsecureSkipVerify()`)
- `Must*` prefix for panicking variants (e.g., `MustGetSecret()`)
- Panic at startup when production has insecure config

### 4.3 Go-Specific Security Advantages

| Advantage | Detail |
|-----------|--------|
| **Memory safety** | No buffer overflows, no use-after-free, no dangling pointers. Go's garbage collector and bounds checking eliminate entire classes of vulnerabilities. |
| **Static typing** | Type mismatches caught at compile time. No implicit type coercion vulnerabilities. |
| **No uninitialized variables** | Zero-value initialization prevents info leaks from uninitialized memory. |
| **Goroutine isolation** | Lightweight concurrency without shared memory by default. Race detector for development. |
| **Compiled binary** | Single static binary — no interpreter to exploit, no runtime dependencies. |
| **Standard library** | Production-grade crypto, HTTP, TLS, and encoding libraries — less dependency on third parties. |
| **Race detector** | Built-in `go test -race` catches data races in development. |
| **Vet tool** | `go vet` catches common mistakes at build time. |
| **Module system** | Dependency versioning and integrity verification built into the toolchain. |
| **Govulncheck** | Official vulnerability scanner integrated into the Go toolchain. |

**What Go does NOT protect against:**
- Business logic errors (still need application-level checks)
- Timing attacks (need constant-time comparison for secrets)
- SQL injection (still need parameterized queries)
- XSS (still need auto-escaping)
- Access control (still need authorization middleware)
- Supply chain attacks (still need dependency auditing)

### 4.4 Go Crypto Standard Library

#### `crypto/*` — What's Available and What's Good

| Package | What It Does | Security Rating | Use For |
|---------|-------------|-----------------|---------|
| `crypto/aes` | AES block cipher | ✅ Excellent | Symmetric encryption |
| `crypto/cipher` | GCM, CBC modes | ✅ Excellent | Authenticated encryption (use GCM) |
| `crypto/ecdsa` | ECDSA signing | ✅ Excellent | JWT signing, certificate signing |
| `crypto/ed25519` | Ed25519 signing | ✅ Excellent | Fast, secure signatures |
| `crypto/elliptic` | Elliptic curves | ✅ Good | Key exchange |
| `crypto/hmac` | HMAC | ✅ Excellent | Message authentication |
| `crypto/rand` | CSPRNG | ✅ Excellent | All random number needs |
| `crypto/rsa` | RSA signing/encryption | ✅ Good (with OAEP) | Legacy systems, JWT signing |
| `crypto/sha256` | SHA-256 | ✅ Excellent | General-purpose hashing |
| `crypto/sha512` | SHA-512 | ✅ Excellent | Higher-security hashing |
| `crypto/tls` | TLS implementation | ✅ Excellent | HTTPS |
| `crypto/subtle` | Constant-time ops | ✅ Essential | Timing-attack-safe comparison |

#### `golang.org/x/crypto/*` — Extended Crypto

| Package | What It Does | Use For |
|---------|-------------|---------|
| `x/crypto/argon2` | Argon2id key derivation | ✅ **Password hashing (recommended)** |
| `x/crypto/bcrypt` | bcrypt password hashing | ✅ Password hashing (widely compatible) |
| `x/crypto/scrypt` | scrypt key derivation | ✅ Password hashing |
| `x/crypto/ssh` | SSH protocol | SSH client/server |
| `x/crypto/nacl/*` | NaCl/libsodium primitives | Secret-key encryption, authentication |
| `x/crypto/acme` | ACME (Let's Encrypt) | Auto TLS certificates |

#### What NOT to Use

| Package | Why Avoid | Use Instead |
|---------|-----------|-------------|
| `crypto/md5` | Collision attacks, preimage attacks | `crypto/sha256` |
| `crypto/sha1` | Collision attacks (SHAttered, 2017) | `crypto/sha256` |
| `crypto/des` | 56-bit key, brute-forceable | `crypto/aes` |
| `crypto/rc4` | Multiple practical attacks | `crypto/aes` + GCM |
| `math/rand` | Not cryptographically secure | `crypto/rand` |
| `encoding/base64` of passwords | Encoding ≠ encryption | `x/crypto/argon2` |

**GoFastr crypto helpers to provide:**
```go
gofastr.HashPassword(password string) (hash string, err error)          // argon2id
gofastr.CheckPassword(password, hash string) (match bool, err error)   // constant-time
gofastr.Encrypt(plaintext []byte, key *[32]byte) (ciphertext []byte)   // AES-256-GCM
gofastr.Decrypt(ciphertext []byte, key *[32]byte) (plaintext []byte)   // AES-256-GCM
gofastr.GenerateToken(n int) (string, error)                            // crypto/rand hex
gofastr.RandomBytes(n int) ([]byte, error)                              // crypto/rand
gofastr.ConstantTimeEqual(a, b string) bool                             // subtle.ConstantTimeCompare
```

---

## 5. Security Checklist: Day 1 vs. Later

### 🔴 Day 1 — MUST HAVE (Non-Negotiable)

These are table stakes. Without these, the framework is irresponsible to use in production.

| # | Feature | Priority | Rationale |
|---|---------|----------|-----------|
| 1 | **HTTPS enforcement + HSTS** | P0 | Everything else is meaningless without transport security |
| 2 | **Secure cookie defaults** (Secure, HttpOnly, SameSite=Lax) | P0 | Session security foundation |
| 3 | **CSRF protection** (token-based + SameSite) | P0 | #1 attack on state-changing requests |
| 4 | **XSS auto-escaping** (html/template) | P0 | #1 attack on rendered content |
| 5 | **Security headers** (full Helmet set) | P0 | Defense in depth for browser-level attacks |
| 6 | **Parameterized queries / query builder** | P0 | SQL injection is still #1 injection attack |
| 7 | **Input validation** (struct-tag based) | P0 | Garbage in, garbage exploited |
| 8 | **Session management** (encrypted cookies, rotation, timeouts) | P0 | Auth requires secure sessions |
| 9 | **Password hashing** (argon2id) | P0 | Plaintext passwords = catastrophic |
| 10 | **Environment-based config** (dev vs prod behavior) | P0 | Debug mode in production = breach |
| 11 | **Error hiding in production** (no stack traces) | P0 | Info leakage enables targeted attacks |
| 12 | **Request ID middleware** | P0 | Cannot debug or trace without it |
| 13 | **CORS with explicit allowlist** | P0 | `Access-Control-Allow-Origin: *` is a misconfiguration |
| 14 | **Secret management** (env vars, never hardcoded, redaction from logs) | P0 | Committed secrets = eventually leaked secrets |

### 🟡 Month 1 — Should Have (Ship With v0.2)

| # | Feature | Priority | Rationale |
|---|---------|----------|-----------|
| 15 | **Rate limiting** (per-IP, per-route, on auth endpoints) | P1 | Brute force protection |
| 16 | **RBAC middleware** (roles, permissions, route guards) | P1 | Authorization is not optional |
| 17 | **Structured security logging** (auth events, denials, violations) | P1 | Cannot respond to incidents without logs |
| 18 | **CSP with nonce support** | P1 | Strongest XSS prevention |
| 19 | **`gofastr audit` CLI command** | P1 | Catch misconfigurations before production |
| 20 | **Dependency auditing** (govulncheck integration) | P1 | Supply chain is a top attack vector |
| 21 | **Login/register/password-reset flows** (reference implementation) | P1 | Don't make every developer reinvent auth |
| 22 | **Breached password check** (HaveIBeenPwned k-anonymity) | P1 | Stop known-compromised credentials |
| 23 | **Account lockout / progressive delays** | P1 | Credential stuffing protection |
| 24 | **MFA support** (TOTP at minimum) | P1 | Password-only auth is insufficient |

### 🟢 Month 2-3 — Nice to Have (Ship With v0.3+)

| # | Feature | Priority | Rationale |
|---|---------|----------|-----------|
| 25 | **OAuth2/OIDC provider support** | P2 | Social login, enterprise SSO |
| 26 | **WebAuthn/Passkeys** | P2 | Phishing-resistant auth |
| 27 | **JWT support** (access + refresh tokens) | P2 | API-first auth pattern |
| 28 | **ABAC policy engine** | P2 | Complex authorization needs |
| 29 | **SSRF protection** (safe HTTP client) | P2 | For apps making outbound requests |
| 30 | **SRI generation** | P2 | CDN integrity |
| 31 | **SBOM generation** | P2 | Supply chain transparency |
| 32 | **Vault integration** (HashiCorp, AWS, GCP) | P2 | Enterprise secret management |
| 33 | **Encryption at rest helpers** (AES-256-GCM) | P2 | Data protection compliance |
| 34 | **Automatic TLS certificates** (Let's Encrypt ACME) | P2 | Zero-config HTTPS |
| 35 | **Security lint rules** (go vet integration) | P2 | Catch insecure patterns at build time |
| 36 | **CSP report-only mode** | P2 | Safe CSP migration |
| 37 | **Session fingerprinting** (IP + User-Agent binding) | P2 | Session hijacking detection |
| 38 | **Web Application Firewall patterns** (common attack signatures) | P3 | Additional attack surface reduction |

### Architecture Principles (Day 1)

| Principle | Implementation |
|-----------|---------------|
| **Default deny** | Every request is unauthorized until explicitly authorized |
| **Fail closed** | If auth/check fails, deny access — never fail open |
| **Defense in depth** | Multiple overlapping security layers |
| **Least privilege** | Each component has minimal necessary permissions |
| **No security by obscurity** | Security must not depend on hiding implementation details |
| **Explicit is better than implicit** | Opt-in to insecure behavior, not opt-out of security |
| **Secure defaults, configurable** | Safe out of the box, tunable for specific needs |
| **Audit everything** | Every security-relevant action should be logged |

---

## Appendix A: Security Header Quick Reference

```
# Complete recommended header set
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'
Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=(), magnetometer=(), gyroscope=(), accelerometer=(), ambient-light-sensor=()
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Resource-Policy: same-origin
X-Request-ID: <uuid>
Cache-Control: no-store
Pragma: no-cache
```

## Appendix B: Go Crypto Decision Tree

```
Need to hash a PASSWORD?
  → argon2id (golang.org/x/crypto/argon2)
  → bcrypt as fallback (golang.org/x/crypto/bcrypt)

Need to hash DATA (integrity check)?
  → SHA-256 (crypto/sha256)
  → SHA-512 for higher security (crypto/sha512)
  → HMAC-SHA256 for keyed hashing (crypto/hmac)

Need to ENCRYPT data (symmetric)?
  → AES-256-GCM (crypto/aes + crypto/cipher)
  → NEVER use ECB mode
  → NEVER use CBC without authentication

Need to SIGN data (asymmetric)?
  → Ed25519 (crypto/ed25519) — preferred
  → ECDSA P-256 (crypto/ecdsa) — for compatibility
  → RSA-2048+ (crypto/rsa) — legacy only

Need RANDOM bytes?
  → crypto/rand — ALWAYS
  → NEVER use math/rand for security purposes

Need to COMPARE secrets?
  → subtle.ConstantTimeCompare (crypto/subtle)
  → NEVER use == for comparing tokens/passwords/hashes
```

## Appendix C: Incident Response Integration Points

A framework should make incident response easier:

| Capability | Framework Support |
|-----------|------------------|
| **Immediate session revocation** | `app.RevokeAllSessions(userID)` |
| **Emergency rate limit tightening** | `app.SetGlobalRateLimit(10, time.Minute)` |
| **Feature kill switch** | `app.DisableRoute("/api/export")` |
| **IP blocking** | `app.BlockIPs(maliciousIPs...)` |
| **Audit log export** | `app.ExportAuditLog(since, filters)` |
| **Security header tightening** | `app.SetCSP(emergencyCSP)` |
| **Maintenance mode** | `app.EnterMaintenanceMode()` |

---

*This document is a living reference. Update as new vulnerabilities emerge and as GoFastr's security architecture evolves.*
*Last updated: 2025-05-05*
