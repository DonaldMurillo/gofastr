# GoFastr Blog Example

A minimal blog application demonstrating JSON entity declarations, CRUD routes,
soft delete, custom endpoints, generated OpenAPI, and entity MCP tools.

## Quick Start

```bash
# From the repository root:
go run examples/blog/main.go
```

The server starts on **http://localhost:8080**.

The example uses SQLite at `./blog.db` and auto-migrates on startup.

## Endpoints

### Users

| Method   | Path             | Auth | Description        |
| -------- | ---------------- | ---- | ------------------ |
| GET      | `/users`         | No   | List users         |
| GET      | `/users/{id}`    | No   | Get user by ID     |
| POST     | `/users`         | Yes  | Create user        |
| PUT      | `/users/{id}`    | Yes  | Update user        |
| PATCH    | `/users/{id}`    | Yes  | Sparse-update user |
| DELETE   | `/users/{id}`    | Yes  | Delete user        |

### Posts

| Method   | Path                  | Auth | Description                  |
| -------- | --------------------- | ---- | ---------------------------- |
| GET      | `/posts`              | No   | List posts (paginated)       |
| GET      | `/posts/{id}`         | No   | Get post by ID               |
| GET      | `/posts/published`    | No   | List published posts only    |
| GET      | `/posts/search?q=...` | No   | Search indexed posts         |
| POST     | `/posts`              | Yes  | Create post                  |
| PUT      | `/posts/{id}`         | Yes  | Update post                  |
| PATCH    | `/posts/{id}`         | Yes  | Sparse-update post           |
| DELETE   | `/posts/{id}`         | Yes  | Soft-delete post             |

### Comments

| Method   | Path                | Auth | Description          |
| -------- | ------------------- | ---- | -------------------- |
| GET      | `/comments`         | No   | List comments        |
| GET      | `/comments/{id}`    | No   | Get comment by ID    |
| POST     | `/comments`         | Yes  | Create comment       |
| PUT      | `/comments/{id}`    | Yes  | Update comment       |
| PATCH    | `/comments/{id}`    | Yes  | Sparse-update comment |
| DELETE   | `/comments/{id}`    | Yes  | Delete comment       |

### Auth

This example does not enforce auth. Production apps should add auth middleware
or per-entity access control before exposing write endpoints.

Create, get, PUT, and PATCH return `{"data": {...}}`; list responses return
`{"data": [...]}` with pagination metadata.

```bash
curl -H "Content-Type: application/json" \
     -d '{"name":"Ada","email":"ada@example.com"}' \
     http://localhost:8080/users
```

### Filtering & Pagination

All list endpoints support query parameters:

```
GET /posts?page=2&limit=10&sort=-created_at&status=published
```

| Param      | Example          | Description                     |
| ---------- | ---------------- | ------------------------------- |
| `page`     | `page=2`         | Page number (default 1)         |
| `limit`    | `limit=10`       | Items per page (max 100)        |
| `sort`     | `sort=-title`    | Sort field (`-` for descending) |
| `{field}`  | `status=draft`   | Exact-match filter              |
| `{field}_like` | `title_like=go` | Contains filter             |

## Entities & Relationships

```
User ──< Post ──< Comment
```

- **User** has many **Posts** and **Comments** (via `author_id`)
- **Post** belongs to **User** (author), has many **Comments**, and supports soft-delete
- **Comment** belongs to both **Post** and **User**

## Declarations

The entities are declared in Go in [`main.go`](main.go) via
`app.Entity("posts", framework.EntityConfig{…})`, so `go run ./examples/blog`
runs with no external files. The same schema is mirrored in
[`gofastr.yml`](gofastr.yml) — the blueprint format the CLI generates from:

```yaml
entities:
  - name: posts
    crud: true
    mcp: true
    soft_delete: true
    fields:
      - { name: title,  type: string, required: true }
      - { name: body,   type: text }
      - { name: status, type: enum, values: [draft, published] }
```

Generate Go from the blueprint:

```bash
cd examples/blog
gofastr generate --from=gofastr.yml
```

## Search

The blog uses `battery/search` with the in-memory backend for `/posts/search`.
Production apps can swap this interface for a Postgres full-text, Meilisearch,
or Elasticsearch backend.

## MCP Tools

Each entity sets `mcp: true`, so GoFastr registers CRUD tools:

| Tool            | Description              | Parameters              |
| --------------- | ------------------------ | ----------------------- |
| `posts_list`    | List posts               | filters, page, limit    |
| `posts_get`     | Get a post               | `id`                    |
| `posts_create`  | Create a post            | writable post fields    |
| `posts_update`  | Update a post            | `id` + writable fields  |
| `posts_delete`  | Delete a post            | `id`                    |

The same pattern exists for `users` and `comments`.
