# GoFastr blog example

A minimal blog application: three entities declared in Go, auto-mounted
CRUD routes, RBAC-gated writes, soft delete, custom endpoints, generated
OpenAPI, and entity MCP tools.

## Quick start

```bash
# From the repository root:
go run ./examples/blog
```

The server starts on **http://localhost:8080**.

The example uses SQLite at `./blog.db` and auto-migrates on startup.

## Endpoints

The Auth column names the role scope a request needs. A request with no
matching scope gets 403; there is no anonymous write anywhere.

### Users

Email is PII, so every operation on users is gated, reads included.

| Method   | Path             | Auth          | Description        |
| -------- | ---------------- | ------------- | ------------------ |
| GET      | `/users`         | `users:read`  | List users         |
| GET      | `/users/{id}`    | `users:read`  | Get user by ID     |
| POST     | `/users`         | `users:write` | Create user        |
| PUT      | `/users/{id}`    | `users:write` | Update user        |
| PATCH    | `/users/{id}`    | `users:write` | Sparse-update user |
| DELETE   | `/users/{id}`    | `users:admin` | Delete user        |

### Posts

| Method   | Path                  | Auth          | Description                  |
| -------- | --------------------- | ------------- | ---------------------------- |
| GET      | `/posts`              | public        | List posts (paginated)       |
| GET      | `/posts/{id}`         | public        | Get post by ID               |
| GET      | `/posts/published`    | public        | List published posts only    |
| GET      | `/posts/search?q=...` | public        | Search indexed posts         |
| POST     | `/posts`              | `posts:write` | Create post                  |
| PUT      | `/posts/{id}`         | `posts:write` | Update post                  |
| PATCH    | `/posts/{id}`         | `posts:write` | Sparse-update post           |
| DELETE   | `/posts/{id}`         | `posts:admin` | Soft-delete post             |

### Comments

| Method   | Path                | Auth             | Description           |
| -------- | ------------------- | ---------------- | --------------------- |
| GET      | `/comments`         | public           | List comments         |
| GET      | `/comments/{id}`    | public           | Get comment by ID     |
| POST     | `/comments`         | `comments:write` | Create comment        |
| PUT      | `/comments/{id}`    | `comments:write` | Update comment        |
| PATCH    | `/comments/{id}`    | `comments:write` | Sparse-update comment |
| DELETE   | `/comments/{id}`    | `comments:admin` | Delete comment        |

### Auth

The scopes come from each entity's `Exposure.Access` block in `main.go`
and mirror the `access:` block in `gofastr.yml`. The example wires no
login, so every gated route answers 403 until you add `battery/auth` (or
any middleware that puts roles on the request context). `blog_test.go`
pins the fail-closed behaviour, and the comment above `registerEntities`
explains why the two declarations must agree on exposure.

Create, get, PUT, and PATCH return `{"data": {...}}`; list responses return
`{"data": [...]}` with pagination metadata.

```bash
# Public read:
curl http://localhost:8080/posts
# Gated write: 403 until a role with posts:write is on the request
curl -H "Content-Type: application/json" \
     -d '{"title":"Hello","author_id":"u1"}' \
     http://localhost:8080/posts
```

### Filtering & pagination

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
| `{field}_like` | `title_like=go` | Contains filter (wildcards escaped) |
| `{field}_in` | `status_in=draft,published` | Any-of filter    |

## Entities & relationships

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
[`gofastr.yml`](gofastr.yml), the blueprint format the CLI generates from:

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

## MCP tools

Each entity sets `mcp: true`, so GoFastr registers CRUD tools:

| Tool            | Description              | Parameters              |
| --------------- | ------------------------ | ----------------------- |
| `posts_list`    | List posts               | filters, page, limit    |
| `posts_get`     | Get a post               | `id`                    |
| `posts_create`  | Create a post            | writable post fields    |
| `posts_update`  | Update a post            | `id` + writable fields  |
| `posts_delete`  | Delete a post            | `id`                    |

The same pattern exists for `users` and `comments`.
