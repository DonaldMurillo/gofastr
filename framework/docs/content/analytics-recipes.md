# Analytics recipes

GoFastr gives you two clean ways to add product and traffic analytics without breaking your strict `default-src 'self'` CSP or shipping heavy client bundles: first-party proxying for external analytics vendors, or lightweight server-side SQL rollups inside your own database.

## Recipe 1: First-party external analytics (`battery/relay`)

If you use Plausible, Umami, PostHog, or another hosted analytics tool, proxy its script and beacon endpoints through your own origin with [`battery/relay`](relay.md):

- Keeps `default-src 'self'` intact (no third-party `script-src` or `connect-src` exceptions).
- Strips cookies, `Authorization`, and `X-Gofastr-*` headers before forwarding so your users' session tokens never reach the analytics vendor.
- Avoids domain-based ad-blocker rules by serving from a neutral path on your own host (default `/__gofastr/t/...`).
- Remember to exempt the relay mount from global CSRF middleware (`auth.WithCSRFSkipPaths("/__gofastr/t/")`) because analytics beacons are cookie-free POSTs.

## Recipe 2: Zero-vendor server-side pageviews and events

For prototypes and small-to-medium apps, recording key business events directly in SQLite or PostgreSQL takes ~30 lines of Go and gives you 100% accuracy (unaffected by ad blockers or JS failures):

1. Declare an `analytics_events` entity with `CRUD: false` (Go-only, never directly writable over public CRUD) containing `event`, `path`, `user_id`, `tenant_id`, and `created_at`.
2. Record domain events (signups, upgrades, exports) in `AfterCreate` hooks or handlers, and pageviews in lightweight middleware (skipping `/__gofastr/`, static assets, and bots).
3. Aggregate daily rollups with a scheduled job (`framework.Scheduler` or `battery/queue` scheduler) into a small `daily_metrics` summary table so dashboard charts stay sub-millisecond.
4. Render dashboards with `ui.StatCard` and `ui.BarChart` / `ui.LineChart`, using `data-cui-poll` if you want periodic auto-refresh without SSE (see [Reactivity](reactivity.md)).

## Recipe 3: Operational metrics and traces

For request latency, error rates, and SQL/job timing — not product funnels — use GoFastr's OpenTelemetry and Prometheus hooks instead of building custom event tables. See [Observability](observability.md) and [Telemetry](telemetry.md).

## Common mistakes

- **Weakening your CSP to `'unsafe-inline'` just to paste an analytics snippet:** serve external scripts through `battery/relay` or `WithExtraScripts` instead.
- **Querying raw `analytics_events` with `GROUP BY` on every dashboard page load:** roll up daily aggregates in a cron/queue job once your table grows past tens of thousands of rows.
- **Logging raw PII (emails, IPs, query-string tokens) into analytics tables:** store hashed or bucketed values and register an eraser if any per-user analytics data is retained (see [Data export & erasure](data-export.md)).
- **Gating first-party relay beacons behind `RequireSession`:** drops all logged-out marketing traffic.
