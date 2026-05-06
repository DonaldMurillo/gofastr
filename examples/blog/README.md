# GoFastr Blog Example

A minimal blog application demonstrating the GoFastr framework's entity definitions, relationships, CRUD routes, custom endpoints, MCP tools, and middleware.

## Quick Start

```bash
# From the repository root:
go run examples/blog/main.go
```

The server starts on **http://localhost:8080**.

> **Note:** CRUD endpoints require a running PostgreSQL database.  Update the connection string in `main.go` to match your environment.  Custom endpoints (`/posts/published`, `/posts/search`) respond even without a database.

## Endpoints

### Users

| Method   | Path             | Auth | Description        |
| -------- | ---------------- | ---- | ------------------ |
| GET      | `/users`         | No   | List users         |
| GET      | `/users/{id}`    | No   | Get user by ID     |
| POST     | `/users`         | Yes  | Create user        |
| PUT      | `/users/{id}`    | Yes  | Update user        |
| DELETE   | `/users/{id}`    | Yes  | Delete user        |

### Posts

| Method   | Path                  | Auth | Description                  |
| -------- | --------------------- | ---- | ---------------------------- |
| GET      | `/posts`              | No   | List posts (paginated)       |
| GET      | `/posts/{id}`         | No   | Get post by ID               |
| GET      | `/posts/published`    | No   | List published posts only    |
| GET      | `/posts/search?q=...` | No   | Search posts by keyword      |
| POST     | `/posts`              | Yes  | Create post                  |
| PUT      | `/posts/{id}`         | Yes  | Update post                  |
| DELETE   | `/posts/{id}`         | Yes  | Soft-delete post             |

### Comments

| Method   | Path                | Auth | Description          |
| -------- | ------------------- | ---- | -------------------- |
| GET      | `/comments`         | No   | List comments        |
| GET      | `/comments/{id}`    | No   | Get comment by ID    |
| POST     | `/comments`         | Yes  | Create comment       |
| PUT      | `/comments/{id}`    | Yes  | Update comment       |
| DELETE   | `/comments/{id}`    | Yes  | Delete comment       |

### Auth

Write endpoints (POST, PUT, DELETE) require an `Authorization` header:

```bash
curl -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
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

## JSON DSL

The same application can be declared as a JSON document (see `gofastr.json`).
This format is designed for AI code generation — describe your entities and
relationships, and the framework builds the app:

```json
{
  "entities": {
    "posts": {
      "fields": [
        { "name": "title", "type": "string", "required": true },
        { "name": "body",  "type": "text" },
        { "name": "status", "type": "enum", "values": ["draft", "published"] }
      ],
      "relations": [
        { "type": "belongsTo", "name": "author", "entity": "users", "foreignKey": "author_id" }
      ],
      "crud": true,
      "mcp": true
    }
  }
}
```

## MCP Tools

The blog registers an MCP tool for post search:

| Tool            | Description              | Parameters              |
| --------------- | ------------------------ | ----------------------- |
| `search_posts`  | Search posts by keyword  | `q` (string, required)  |
