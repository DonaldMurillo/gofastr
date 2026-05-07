# GoFastr SPA Example

A single-page application demo with **Vue 3 + Vue Router 4** (loaded from jsdelivr CDN, zero build step) backed by a GoFastr JSON API.

## Architecture

- **Backend**: GoFastr entities auto-generate JSON CRUD endpoints under `/api/articles`, `/api/projects`
- **Frontend**: Vue 3 + Vue Router 4 from jsdelivr CDN — no npm, no build step
- **Routing**: Vue Router with `createWebHistory()` — real URLs, not hash fragments
- **Serving**: Static files with **SPA mode** — unmatched paths fall back to `index.html`

## How it works

Entity CRUD routes are mounted under `/api/` so they don't conflict with Vue Router's client-side routes:

| Server Route | Type | Handler |
|---|---|---|
| `/` | SPA | Serves `index.html` |
| `/articles` | SPA | Serves `index.html` → Vue Router renders `ArticleList` |
| `/articles/:id` | SPA | Serves `index.html` → Vue Router renders `ArticleDetail` |
| `/projects` | SPA | Serves `index.html` → Vue Router renders `ProjectList` |
| `/about` | SPA | Serves `index.html` → Vue Router renders `About` |
| `/api/articles` | API | JSON — list articles |
| `/api/articles/:id` | API | JSON — get article |
| `/api/projects` | API | JSON — list projects |
| `/api/site` | API | JSON — site metadata |
| `/style.css`, `/app.js` | Static | Served from `static/` dir |

## Running

```bash
cd examples/spa
go run main.go
# Open http://localhost:3090
```

## Tech

- **Vue 3** — `https://cdn.jsdelivr.net/npm/vue@3`
- **Vue Router 4** — `https://cdn.jsdelivr.net/npm/vue-router@4`
- **GoFastr** — entity CRUD, auto-migration, SPA static serving
